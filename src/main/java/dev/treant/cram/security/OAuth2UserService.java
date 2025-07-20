package dev.treant.cram.security;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.treant.cram.model.User;
import dev.treant.cram.repository.UserRepository;
import org.springframework.http.*;
import org.springframework.security.oauth2.client.userinfo.*;
import org.springframework.security.oauth2.core.*;
import org.springframework.security.oauth2.core.user.*;
import org.springframework.stereotype.Service;
import org.springframework.web.client.RestTemplate;

import java.util.*;

@Service
public class OAuth2UserService extends DefaultOAuth2UserService {

    private final UserRepository userRepository;
    private final RestTemplate restTemplate = new RestTemplate();
    private final ObjectMapper objectMapper = new ObjectMapper();

    public OAuth2UserService(UserRepository userRepository) {
        this.userRepository = userRepository;
    }

    @Override
    public OAuth2User loadUser(OAuth2UserRequest request) throws OAuth2AuthenticationException {
        OAuth2User principal = super.loadUser(request);
        Map<String, Object> attributes = new HashMap<>(principal.getAttributes());

        String provider = request.getClientRegistration().getRegistrationId();
        String providerId = String.valueOf(attributes.get("id"));
        String login = String.valueOf(attributes.get("login"));
        String name = String.valueOf(attributes.get("name"));
        String avatar = String.valueOf(attributes.get("avatar_url"));
        String email = extractEmail(attributes, request, provider, attributes);

        User user = userRepository.findByProviderAndProviderId(provider, providerId)
                .map(u -> {
                    u.setLogin(login);
                    u.setName(name);
                    u.setEmail(email);
                    u.setAvatarUrl(avatar);
                    return u;
                })
                .orElse(User.builder()
                        .provider(provider)
                        .providerId(providerId)
                        .login(login)
                        .name(name)
                        .email(email)
                        .avatarUrl(avatar)
                        .build()
                );

        userRepository.save(user);

        return new DefaultOAuth2User(principal.getAuthorities(), attributes, "login");
    }

    private String extractEmail(
            Map<String, Object> attributes,
            OAuth2UserRequest request,
            String provider,
            Map<String, Object> merged
    ) {
        String email = (String) attributes.get("email");

        if (email == null && "github".equalsIgnoreCase(provider)) {
            email = fetchPrimaryEmailFromGitHub(request.getAccessToken().getTokenValue());
            merged.put("email", email);
        }

        return email;
    }

    private String fetchPrimaryEmailFromGitHub(String token) {
        HttpHeaders headers = new HttpHeaders();
        headers.setBearerAuth(token);
        HttpEntity<?> entity = new HttpEntity<>(headers);

        ResponseEntity<String> response = restTemplate.exchange(
                "https://api.github.com/user/emails",
                HttpMethod.GET,
                entity,
                String.class
        );

        try {
            JsonNode list = objectMapper.readTree(response.getBody());
            for (JsonNode node : list) {
                if (node.get("primary").asBoolean() && node.get("verified").asBoolean()) {
                    return node.get("email").asText();
                }
            }
        } catch (Exception e) {
            e.printStackTrace();
        }

        return null;
    }
}