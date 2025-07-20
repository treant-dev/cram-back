package dev.treant.cram.model;

import jakarta.persistence.*;
import lombok.*;

import java.time.Instant;

@Entity
@Table(name = "users", uniqueConstraints = {
        @UniqueConstraint(columnNames = {"provider", "providerId"})
})
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class User {
    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    private String provider;
    private String providerId;
    private String login;
    private String name;
    private String email;
    private String avatarUrl;

    private Instant lastLoginAt;

    @PreUpdate
    public void updateTimestamp() {
        this.lastLoginAt = Instant.now();
    }
}