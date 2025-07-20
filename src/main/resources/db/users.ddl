CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    provider VARCHAR(50) NOT NULL,
    provider_id VARCHAR(255) NOT NULL,
    login VARCHAR(100),
    name VARCHAR(255),
    email VARCHAR(255),
    avatar_url TEXT,
    last_login_at TIMESTAMP,

    CONSTRAINT uq_users_provider_provider_id UNIQUE (provider, provider_id)
);