package dev.treant.cram;

import org.springframework.security.oauth2.core.user.OAuth2User;
import org.springframework.security.core.annotation.AuthenticationPrincipal;
import org.springframework.web.bind.annotation.*;

import java.util.HashMap;
import java.util.Map;

@RestController
public class UserController {

    @GetMapping("/public")
    public String publicEndpoint() {
        return "This is a public endpoint.";
    }

    @GetMapping("/me")
    public Map<String, Object> user(@AuthenticationPrincipal OAuth2User principal) {
        Map<String, Object> result = new HashMap<>();
        result.put("login", principal.getAttribute("login"));
        result.put("name", principal.getAttribute("name"));
        result.put("email", principal.getAttribute("email"));
        result.put("avatar_url", principal.getAttribute("avatar_url"));
        return result;
    }
}
