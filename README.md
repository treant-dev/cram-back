# cram-back

Go backend for the CRAM flashcard study platform.

---

## Functional Requirements: Role-Based Access Control

### Overview

Access control in CRAM is split into two independent dimensions:

- **Role** — the type of user (`user`, `admin`). Controls what routes and actions are accessible.
- **Plan** — the subscription tier (`free`, `pro`). Controls which features are available.

These are stored as separate columns on the `users` table and included in the JWT claims on every authenticated request.

---

### Roles

#### `user` (default)

- Can create, read, update, and delete their own study sets.
- Can create, read, update, and delete cards and test questions within their own sets.
- Cannot access or modify another user's sets, cards, or test questions.
- Cannot access any `/admin/*` routes.

#### `admin`

- Inherits all `user` permissions.
- Can list all users in the system.
- Can delete any study set regardless of ownership.
- Can change a user's role or plan.
- Can access all `/admin/*` routes.

#### Bootstrapping the first admin

Admin accounts are promoted directly in the database — there is no API endpoint for self-promotion:

```sql
UPDATE users SET role = 'admin' WHERE email = 'your@email.com';
```

---

### Plans

#### `free` (default)

- Can create up to **10** study sets.
- Can add up to **100** cards per set.
- Can add up to **50** test questions per set.
- CSV import limited to **100 rows** per file.

#### `pro`

- Unlimited study sets.
- Unlimited cards and test questions per set.
- CSV import up to **5 000 rows** per file.
- (Future) access to AI-generated card suggestions.

---

### Rules and invariants

1. Role and plan are independent — an admin can be on the free plan; a pro user is not an admin.
2. Permissions are enforced on the backend at every request. Frontend hiding of UI elements is cosmetic only.
3. Role and plan are embedded in the JWT at login time. Changes take effect when the user's token expires and they log in again (24 h TTL). For immediate revocation, see token invalidation (not yet implemented).
4. Ownership checks always apply, even for admins acting on their own resources. Admins bypass ownership checks only through explicit admin endpoints, not the regular user endpoints.

---

### Route structure (planned)

| Group | Middleware | Description |
|---|---|---|
| `GET /sets`, `POST /sets`, etc. | `RequireAuth` | Standard user endpoints — ownership enforced in service layer |
| `GET /admin/users` | `RequireAuth` + `RequireRole("admin")` | List all users |
| `DELETE /admin/sets/{setID}` | `RequireAuth` + `RequireRole("admin")` | Delete any set |
| `PUT /admin/users/{userID}` | `RequireAuth` + `RequireRole("admin")` | Update role or plan |

Plan limits are enforced inside the service layer, not at the router level.

---

### Schema changes required

```sql
ALTER TABLE users
    ADD COLUMN role TEXT NOT NULL DEFAULT 'user'
        CHECK (role IN ('user', 'admin')),
    ADD COLUMN plan TEXT NOT NULL DEFAULT 'free'
        CHECK (plan IN ('free', 'pro'));
```

---

### Out of scope (for now)

- Resource-level sharing (inviting another user to edit a set).
- Public sets accessible without login.
- Payment integration for plan upgrades.
- Token revocation / session invalidation on role change.
