---
# tnib-e001
version: 2
title: User Authentication
status: in-progress
type: epic
priority: high
created_at: 2026-02-18T10:00:00Z
updated_at: 2026-03-24T16:00:00Z
documents:
    - docs/auth-architecture.md
milestone: tnib-m001
milestone_order: a
---

## Summary

Every other feature in v1.0 assumes we know who the user is, so authentication is the first thing a new team touches and the last thing they should ever have to think about. This epic covers the full lifecycle of an account: creating it, signing in with a password or a social provider, staying signed in across devices, and recovering access when a password is forgotten. It also sets the security baseline the rest of the product inherits, which is why it carries `high` priority even though most of the work is unglamorous.

The design follows the [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html) wherever it gives a concrete rule, and the architecture is written up in `docs/auth-architecture.md`. When the two disagree, this document wins and the architecture doc gets a correction.

## Problem

Early users signed up through a temporary invite flow that stored passwords with a fast hash and kept sessions alive forever. Support tickets show three recurring pains:

1. **People forget passwords** and have no way back in except emailing support, which takes a day on average.
2. **Teams want single sign-on** with the accounts they already have; roughly 60% of trial teams asked for Google or GitHub login in the first week.
3. **Sessions never end.** A password change does not sign out other devices, which [[tnib-b002]] tracks as a security bug.

> "I changed my password because I thought someone had my laptop, and the laptop was still logged in the next morning."
> — trial user, support ticket

## Goals and non-goals

**Goals**

- Email and password sign-in that meets the OWASP baseline (#tnib-f001), with hashing done by #tnib-t001 and brute-force protection by #tnib-t002.
- Social sign-in through Google and GitHub using OAuth 2.0 with PKCE ([[tnib-f002]]); the library choice is the subject of #tnib-r001.
- Self-service password reset by email ([[tnib-f003]]).
- Sessions that end when they should: on sign-out, on password change, and after 30 days of inactivity.

**Non-goals**

- Multi-factor authentication. It is planned, but not for v1.0.
- SAML or enterprise SSO, which belongs to the enterprise tier.
- Passwordless magic links. Worth revisiting once reset emails prove deliverable.

## User stories

- As a new user, I can create an account with my email and a password, and I am told immediately if the password is too weak, so I do not find out at the next sign-in.
- As a returning user, I can sign in with Google or GitHub, so I do not need another password.
- As someone who forgot their password, I can request a reset link that works once and expires after an hour.
- As a security-conscious user, changing my password signs out every other session, so a lost device stops working.

## Requirements

| Area | Requirement | Owner | Notes |
|---|---|---|---|
| Passwords | bcrypt, cost 12, rehash on sign-in when the cost rises | #tnib-t001 | Cost is reviewed yearly against login latency |
| Rate limiting | 5 failed attempts per account per 15 minutes, then exponential backoff | #tnib-t002 | Limit per account and per IP, never only per IP |
| OAuth | Authorization code flow with PKCE; state and nonce checked | #tnib-t004, #tnib-t005 | Callback URL mismatch is #tnib-b001 |
| Tokens | Access token 15 minutes, refresh token 30 days, rotated on use | #tnib-t007 | See the token shape below |
| Reset | Single-use link, 1 hour expiry, does not reveal whether the email exists | #tnib-t006 | Template copy reviewed by support |
| Docs | Public API reference for every auth endpoint | #tnib-t008 | Published with the v1.0 API docs |

Non-functional: sign-in completes within 300 ms at the 95th percentile excluding the provider round trip, and no endpoint returns a different error for "unknown email" and "wrong password". Integration coverage for the whole flow lives in #tnib-t003.

## Design notes

Access tokens are short-lived JWTs signed with a rotating key; refresh tokens are opaque, stored hashed, and bound to a session row so that revoking the session revokes the token. The claims follow [RFC 7519](https://datatracker.ietf.org/doc/html/rfc7519) and the OAuth flow follows [RFC 6749](https://datatracker.ietf.org/doc/html/rfc6749) with the [PKCE extension](https://datatracker.ietf.org/doc/html/rfc7636).

```json
{
  "sub": "user_8f3a",
  "sid": "sess_41c2",
  "iat": 1774353600,
  "exp": 1774354500,
  "scope": ["tasks:read", "tasks:write"]
}
```

Revocation is by session id (`sid`): a password change deletes every session row for the user except the current one, and the next refresh with a deleted `sid` fails. That closes #tnib-b002 without a token denylist.

## Rollout

1. Ship email and password sign-in behind the existing invite gate, and migrate old hashes on first sign-in.
2. Enable Google, then GitHub, one week apart, watching the callback error rate.
3. Turn on password reset once delivery to the three largest mail providers is confirmed.
4. Expire every pre-migration session at the end of the rollout window.

## Risks

- **Provider outages** block social sign-in entirely; users who only ever used Google cannot fall back to a password. Mitigation: offer to set a password after the first social sign-in.
- **Email deliverability** decides whether reset works at all. We do not control spam filters, so the reset page says where to look and how long to wait.
- **Clock skew** between services can reject fresh tokens; allow 30 seconds of leeway.

## Objective

Implement a complete authentication system supporting email/password, OAuth2 social providers, and secure session management.

## Acceptance Criteria

- [x] Users can register and log in with email/password
- [x] Passwords hashed with bcrypt (cost 12)
- [ ] OAuth2 login via Google and GitHub
- [ ] Password reset via email link
- [ ] Sessions invalidated on password change

## Scope Boundaries

- MFA is out of scope for v1.0
- SAML/SSO deferred to enterprise tier

## Open questions

- Should a social-only account be able to add a password later, or only the reverse?
- Do we keep the 30-day refresh window for mobile, where re-entering a password is more painful?
