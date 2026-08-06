package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
)

type OAuthHandler struct {
	svc *service.OAuthService
}

func NewOAuthHandler(svc *service.OAuthService) *OAuthHandler { return &OAuthHandler{svc: svc} }

func (h *OAuthHandler) claims(r *http.Request) *auth.Claims {
	c, _ := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	return c
}

// issuer is this API's public origin — what clients see, not what the process binds to.
func issuer() string {
	if u := os.Getenv("PUBLIC_API_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := os.Getenv("GOOGLE_REDIRECT_URL"); u != "" {
		// The Google callback lives on this API, so its origin is ours.
		if parsed, err := url.Parse(u); err == nil && parsed.Scheme != "" {
			return parsed.Scheme + "://" + parsed.Host
		}
	}
	return "http://localhost:8080"
}

// oauthError writes the RFC 6749 error body. Descriptions stay generic on anything involving a
// credential: a precise reason tells an attacker which half of the guess was right.
func oauthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

// ---------- metadata ----------

// ProtectedResourceMetadata godoc
// @Summary      OAuth protected resource metadata (RFC 9728)
// @Tags         oauth
// @Produce      json
// @Success      200 {object} map[string]any
// @Router       /.well-known/oauth-protected-resource [get]
func (h *OAuthHandler) ProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 issuer() + "/mcp",
		"authorization_servers":    []string{issuer()},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{model.ScopeRead, model.ScopeReadWrite},
	})
}

// AuthorizationServerMetadata godoc
// @Summary      OAuth authorization server metadata (RFC 8414)
// @Tags         oauth
// @Produce      json
// @Success      200 {object} map[string]any
// @Router       /.well-known/oauth-authorization-server [get]
func (h *OAuthHandler) AuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	iss := issuer()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/oauth/authorize",
		"token_endpoint":                        iss + "/oauth/token",
		"registration_endpoint":                 iss + "/oauth/register",
		"scopes_supported":                      []string{model.ScopeRead, model.ScopeReadWrite},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

// ---------- dynamic client registration ----------

// Register godoc
// @Summary      Register an OAuth client (RFC 7591)
// @Tags         oauth
// @Accept       json
// @Produce      json
// @Success      201 {object} map[string]any
// @Failure      400 {object} map[string]string
// @Router       /oauth/register [post]
func (h *OAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		Scope        string   `json:"scope"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "body must be JSON")
		return
	}
	client, err := h.svc.RegisterClient(r.Context(), req.ClientName, req.RedirectURIs, req.Scope)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ID,
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"scope":                      client.Scope,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

// ---------- authorize ----------

// Authorize godoc
// @Summary      OAuth authorization endpoint
// @Description  Validates the request, then hands off to the consent screen in the web app.
// @Tags         oauth
// @Success      302 {string} string "redirect to consent or back to the client"
// @Router       /oauth/authorize [get]
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req, err := h.svc.ValidateAuthorize(r.Context(),
		q.Get("client_id"), q.Get("redirect_uri"), q.Get("response_type"),
		q.Get("code_challenge"), q.Get("code_challenge_method"), q.Get("scope"), q.Get("resource"))
	if err != nil {
		// Client or redirect_uri could not be verified, so there is nowhere safe to redirect to:
		// show the error here instead of bouncing it to an unvalidated URL.
		if errors.Is(err, service.ErrOAuthInvalidClient) || errors.Is(err, service.ErrOAuthInvalidRedirect) {
			oauthError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		redirectError(w, r, q.Get("redirect_uri"), q.Get("state"), "invalid_request", err.Error())
		return
	}

	// The consent screen lives in the web app; it re-sends these parameters to /oauth/approve.
	// The user's own session is a cookie on this API, so the frontend calls back with credentials.
	consent := frontendURL() + "/oauth/consent?" + url.Values{
		"client_id":             {req.Client.ID},
		"client_name":           {req.Client.Name},
		"redirect_uri":          {req.RedirectURI},
		"response_type":         {"code"},
		"scope":                 {req.Scope},
		"state":                 {q.Get("state")},
		"code_challenge":        {req.CodeChallenge},
		"code_challenge_method": {"S256"},
		"resource":              {req.Resource},
	}.Encode()
	http.Redirect(w, r, consent, http.StatusFound)
}

// Approve godoc
// @Summary      Grant an OAuth client access (called by the consent screen)
// @Tags         oauth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} map[string]string "redirect_to"
// @Router       /oauth/approve [post]
func (h *OAuthHandler) Approve(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		ClientID            string `json:"client_id"`
		RedirectURI         string `json:"redirect_uri"`
		Scope               string `json:"scope"`
		State               string `json:"state"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
		Resource            string `json:"resource"`
		Approved            bool   `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Re-validate everything: the consent screen is a browser page and its parameters are not
	// more trustworthy than the original query string.
	req, err := h.svc.ValidateAuthorize(r.Context(),
		body.ClientID, body.RedirectURI, "code", body.CodeChallenge, body.CodeChallengeMethod, body.Scope, body.Resource)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if !body.Approved {
		writeJSON(w, http.StatusOK, map[string]string{
			"redirect_to": buildRedirect(req.RedirectURI, url.Values{
				"error":             {"access_denied"},
				"error_description": {"the user declined"},
				"state":             {body.State},
			}),
		})
		return
	}

	code, err := h.svc.Approve(r.Context(), req, claims.UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"redirect_to": buildRedirect(req.RedirectURI, url.Values{"code": {code}, "state": {body.State}}),
	})
}

// ---------- token ----------

// Token godoc
// @Summary      OAuth token endpoint
// @Tags         oauth
// @Accept       x-www-form-urlencoded
// @Produce      json
// @Success      200 {object} map[string]any
// @Failure      400 {object} map[string]string
// @Router       /oauth/token [post]
func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "body must be form-encoded")
		return
	}
	clientID := r.PostFormValue("client_id")

	var (
		tokens *service.TokenSet
		err    error
	)
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		tokens, err = h.svc.ExchangeCode(r.Context(), clientID,
			r.PostFormValue("code"), r.PostFormValue("redirect_uri"), r.PostFormValue("code_verifier"))
	case "refresh_token":
		tokens, err = h.svc.Refresh(r.Context(), clientID, r.PostFormValue("refresh_token"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrOAuthInvalidGrant):
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code or refresh token is invalid, expired or already used")
		case errors.Is(err, service.ErrOAuthInvalidClient):
			oauthError(w, http.StatusBadRequest, "invalid_client", "unknown client")
		case errors.Is(err, service.ErrOAuthInvalidRequest):
			oauthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		default:
			oauthError(w, http.StatusInternalServerError, "server_error", "could not issue a token")
		}
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    tokens.ExpiresIn,
		"scope":         tokens.Scope,
	})
}

// ---------- grants, for the settings page ----------

// ListGrants godoc
// @Summary      List connected applications
// @Tags         oauth
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} map[string]any
// @Router       /account/connections [get]
func (h *OAuthHandler) ListGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := h.svc.ListGrants(r.Context(), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(grants))
	for _, g := range grants {
		out = append(out, map[string]any{
			"id":           g.ID,
			"client_name":  g.ClientName,
			"scope":        g.Scope,
			"created_at":   g.CreatedAt,
			"last_used_at": g.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeGrant godoc
// @Summary      Disconnect an application
// @Description  Revokes the grant and every token issued under it, effective immediately.
// @Tags         oauth
// @Security     BearerAuth
// @Param        grantID path string true "Grant ID"
// @Success      204 "revoked"
// @Router       /account/connections/{grantID} [delete]
func (h *OAuthHandler) RevokeGrant(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RevokeGrant(r.Context(), chi.URLParam(r, "grantID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- helpers ----------

func buildRedirect(base string, params url.Values) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for k, vs := range params {
		if len(vs) > 0 && vs[0] != "" {
			q.Set(k, vs[0])
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func redirectError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, description string) {
	if redirectURI == "" {
		oauthError(w, http.StatusBadRequest, code, description)
		return
	}
	http.Redirect(w, r, buildRedirect(redirectURI, url.Values{
		"error": {code}, "error_description": {description}, "state": {state},
	}), http.StatusFound)
}
