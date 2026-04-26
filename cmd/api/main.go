// @title           Cram API
// @version         1.0
// @description     Flashcard study platform API
// @host            localhost:8080
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/joho/godotenv"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/db"
	_ "github.com/treant-dev/cram-go/docs"
	"github.com/treant-dev/cram-go/internal/handler"
	apimiddleware "github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/repository"
	"github.com/treant-dev/cram-go/internal/seed"
	"github.com/treant-dev/cram-go/internal/service"
	"github.com/treant-dev/cram-go/internal/storage"
)

func allowedOrigins() []string {
	origins := []string{"http://localhost:3000"}
	if u := os.Getenv("FRONTEND_URL"); u != "" {
		origins = append(origins, u)
	}
	return origins
}

func main() {
	seedFlag := flag.Bool("seed", false, "seed dev data and enable /auth/dev-login")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found")
	}

	if *seedFlag && os.Getenv("ENV") == "production" {
		log.Fatal("--seed flag must not be used in production")
	}

	for _, key := range []string{"JWT_SECRET", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "DATABASE_URL"} {
		if os.Getenv(key) == "" {
			log.Fatalf("required env var not set: %s", key)
		}
	}
	if os.Getenv("JWT_SECRET") == "change-me" {
		log.Fatal("JWT_SECRET must be changed from the default value")
	}

	ctx := context.Background()

	if err := db.RunMigrations(os.Getenv("DATABASE_URL")); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	pool, err := db.NewPool(ctx)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	userRepo := repository.NewUserRepository(pool)
	collectionRepo := repository.NewCollectionRepository(pool)
	cardRepo := repository.NewCardRepository(pool)
	tqRepo := repository.NewTestQuestionRepository(pool)
	followRepo := repository.NewFollowRepository(pool)
	studyRepo := repository.NewStudyRepository(pool)
	cardSvc := service.NewCollectionService(collectionRepo, cardRepo, tqRepo, followRepo, userRepo, studyRepo)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	r := chi.NewRouter()
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RequestID)
	r.Use(httprate.LimitByIP(300, time.Minute))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	authRateLimit := httprate.LimitByIP(20, time.Minute)
	authHandler := handler.NewAuthHandler(auth.NewGoogleOAuthConfig(), userRepo)
	r.With(authRateLimit).Get("/auth/google", authHandler.GoogleLogin)
	r.With(authRateLimit).Get("/auth/google/callback", authHandler.GoogleCallback)
	r.Get("/auth/logout", authHandler.Logout)
	r.With(apimiddleware.RequireAuth).Get("/auth/me", authHandler.Me)

	cardsHandler := handler.NewCardsHandler(cardSvc)
	usersHandler := handler.NewUsersHandler(cardSvc)
	studyHandler := handler.NewStudyHandler(cardSvc)
	adminHandler := handler.NewAdminHandler(cardSvc)
	accountHandler := handler.NewAccountHandler(cardSvc)
	shareHandler := handler.NewShareHandler(cardSvc)

	minioStore, minioErr := storage.NewMinioStore()
	if minioErr != nil {
		log.Printf("warning: MinIO unavailable (%v) — file uploads and image deletion disabled", minioErr)
	} else {
		cardSvc.SetImageStore(minioStore)
	}
	uploadHandler := handler.NewUploadHandler(minioStore)
	r.Get("/public/collections", cardsHandler.ListPublicCollections)
	r.Get("/shared/{token}", shareHandler.View)
	r.Group(func(r chi.Router) {
		r.Use(apimiddleware.RequireAuth)
		r.Get("/users", usersHandler.ListUsers)
		r.Get("/home", cardsHandler.Home)
		r.Post("/collections", cardsHandler.CreateCollection)
		r.Get("/collections", cardsHandler.ListCollections)
		r.Get("/collections/{collectionID}", cardsHandler.GetCollection)
		r.Put("/collections/{collectionID}", cardsHandler.UpdateCollection)
		r.Delete("/collections/{collectionID}", cardsHandler.DeleteCollection)
		r.Post("/collections/{collectionID}/cards", cardsHandler.AddCard)
		r.Post("/collections/{collectionID}/cards/import", cardsHandler.ImportCSV)
		r.Put("/collections/{collectionID}/cards/{cardID}", cardsHandler.UpdateCard)
		r.Delete("/collections/{collectionID}/cards/{cardID}", cardsHandler.DeleteCard)
		r.Post("/collections/{collectionID}/draft", cardsHandler.GetOrCreateDraft)
		r.Put("/collections/{collectionID}/draft", cardsHandler.UpdateDraft)
		r.Delete("/collections/{collectionID}/draft", cardsHandler.DiscardDraft)
		r.Post("/collections/{collectionID}/draft/publish", cardsHandler.PublishDraft)
		r.Post("/collections/{collectionID}/follow", cardsHandler.Follow)
		r.Delete("/collections/{collectionID}/follow", cardsHandler.Unfollow)
		r.Post("/collections/{collectionID}/tests", cardsHandler.AddTestQuestion)
		r.Put("/collections/{collectionID}/tests/{tqID}", cardsHandler.UpdateTestQuestion)
		r.Delete("/collections/{collectionID}/tests/{tqID}", cardsHandler.DeleteTestQuestion)
		r.Post("/collections/{collectionID}/study", studyHandler.Submit)
		r.Get("/collections/{collectionID}/history", studyHandler.GetHistory)
		r.With(httprate.LimitByIP(30, time.Minute)).Post("/upload", uploadHandler.Upload)
		r.Delete("/account", accountHandler.Delete)
		r.Post("/collections/{collectionID}/share", shareHandler.Generate)
		r.Delete("/collections/{collectionID}/share", shareHandler.Revoke)
	})

	r.Group(func(r chi.Router) {
		r.Use(apimiddleware.RequireAuth)
		r.Use(apimiddleware.RequireRole("admin"))
		r.Get("/admin/users", adminHandler.ListUsers)
		r.Put("/admin/users/{userID}/role", adminHandler.SetRole)
		r.Delete("/admin/collections/{collectionID}", adminHandler.DeleteCollection)
	})

	if *seedFlag {
		devUserID, err := seed.Run(ctx, pool)
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
		adminUserID, err := seed.RunAdmin(ctx, pool)
		if err != nil {
			log.Fatalf("seed admin: %v", err)
		}

		devCookieLogin := func(userID, email, role string) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				token, err := auth.IssueToken(userID, email, role, "")
				if err != nil {
					http.Error(w, "could not issue token", http.StatusInternalServerError)
					return
				}
				secure := os.Getenv("ENV") == "production"
				frontendURL := os.Getenv("FRONTEND_URL")
				if frontendURL == "" {
					frontendURL = "http://localhost:3000"
				}
				http.SetCookie(w, &http.Cookie{
					Name:     "jwt",
					Value:    token,
					HttpOnly: true,
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
					Path:     "/",
					MaxAge:   7 * 24 * 60 * 60,
				})
				http.Redirect(w, r, frontendURL+"/auth/callback", http.StatusTemporaryRedirect)
			}
		}

		r.Get("/auth/dev-login", devCookieLogin(devUserID, seed.DevEmail, "user"))
		r.Get("/auth/dev-admin-login", devCookieLogin(adminUserID, seed.AdminEmail, "admin"))
		log.Println("dev mode: seed data loaded, /auth/dev-login and /auth/dev-admin-login enabled")
	}

	// Periodically evict expired OAuth CSRF states.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			auth.CleanupExpired()
		}
	}()

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Fatal("server forced to shutdown:", err)
	}
	log.Println("server exited")
}
