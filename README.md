# Backend For Cram Project

# Quick Start

* Update `.env` with your data:

  ```sh
  GITHUB_CLIENT_ID=
  GITHUB_CLIENT_SECRET=
  POSTGRES_DB=cram
  POSTGRES_USER=default
  POSTGRES_PASSWORD=default
  ```
  and run

* Start local DB
    ```bash
    docker compose up -d
    ```
* Build
    ```bash
    ./mvnw clean install
    ```
* Run
    ```bash
    export $(grep -v '^#' .env | xargs) && ./mvnw spring-boot:run
    ```
* Try it out on http://localhost:8080