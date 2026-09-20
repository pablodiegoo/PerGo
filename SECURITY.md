# Security Policy — PerGo

The PerGo project takes the security of our CPaaS omnichannel platform and the data entrusted to it very seriously. Because PerGo is an open-source, public repository, strict security practices must be maintained at all times by human contributors and automated AI agents to prevent the exposure of secrets, credentials, and sensitive data.

---

## 1. Supported Versions

We actively provide security patches and updates for the following versions of PerGo:

| Version | Supported          | Status                                 |
| ------- | ------------------ | -------------------------------------- |
| `main`  | :white_check_mark: | Active development & rolling fixes    |
| `v1.x`  | :white_check_mark: | Latest stable releases                 |
| `< 1.0` | :x:                | End of life; please upgrade to `v1.x`  |

---

## 2. Zero-Secret Policy for Public Repositories

**Never commit real secrets or production credentials to this repository.**

This includes, but is not limited to:
- Database passwords and connection strings containing credentials
- API keys and tokens (Stripe, Meta/WhatsApp Cloud, AWS, Telegram, GitHub tokens, etc.)
- Webhook signing secrets
- TLS private keys, certificates, and SSH private keys
- JWT secrets and signing keys
- Production `.env` files

---

## 3. Environment Variables & Secret Injection

- Active environment files (`.env`, `.env.local`, `.env.production`, `.env.staging`) are excluded by `.gitignore` and blocked by pre-commit hooks.
- Commit only template files such as `.env.example` containing non-sensitive dummy placeholders.
- In production, inject secrets via environment variables through your orchestration layer (Docker Compose secrets, Kubernetes Secrets, or a secure secrets manager like Vault/Doppler/1Password).

---

## 4. Mock & Testing Conventions

To prevent secret scanning tools (such as GitHub Secret Scanning, Gitleaks, and TruffleHog) from triggering false positives, adhere to these rules when writing unit tests, fixtures, or documentation:

- **Do NOT emulate provider-specific secret formats:**
  - Avoid `whsec_` followed by 32+ hex characters (Stripe Webhook Signing Secret).
  - Avoid `sk_live_...` or `sk_test_...` with valid length formats.
  - Avoid `AKIA...` (AWS Access Key ID format).
  - Avoid `ghp_...` (GitHub Personal Access Token format).
- **Use explicit mock identifiers:**
  - Use prefixes like `mock_`, `test_`, `dummy_` or non-matching descriptive placeholders:
    ```go
    // Safe
    webhookSecret := "mock_whsec_placeholder_secret"
    apiKey := "pgo_live_mocked1234567890abcdef"
    ```
  - In Markdown documentation, use placeholders like `<WORKSPACE_WEBHOOK_SECRET>` or descriptive dummy values (e.g. `whsec_example_dummy_placeholder`).

---

## 5. Local Developer Guardrails (Git Hooks)

To protect yourself from accidentally committing secrets locally:

1. Run the setup target:
   ```bash
   make hooks
   ```
2. This configures `git` to use the `.githooks/` directory (`git config core.hooksPath .githooks`).
3. The pre-commit hook will:
   - Abort if an active `.env` file is staged.
   - Run `gitleaks protect --staged` using `.gitleaks.toml` if Gitleaks is installed.
   - Run built-in pattern heuristics as a fallback if Gitleaks is not yet installed.

> [!TIP]
> Install Gitleaks globally for maximum local scanning accuracy:
> ```bash
> go install github.com/zricethezav/gitleaks/v8@latest
> ```

---

## 6. Remote CI & GitHub Protections

- **CI Secret Scanning:** Every commit and Pull Request triggers the `secret-scan` job in `.github/workflows/ci.yml`, running Gitleaks against `.gitleaks.toml`. Any detected secret blocks the build and PR merge.
- **GitHub Push Protection:** Push protection is enabled on GitHub for the PerGo repository, blocking pushes containing recognized partner secrets at the `git push` boundary.

---

## 7. Reporting a Vulnerability or Leaked Secret

### Preferred Method: GitHub Private Vulnerability Reporting
1. Navigate to the [PerGo Security Advisories](https://github.com/pablodiegoo/PerGo/security/advisories/new) page.
2. Click on **Report a vulnerability**.
3. Fill out the report form detailing the vulnerability, impact, affected component or channel, and reproduction steps.
4. Submit the report to open a private advisory channel directly with the maintainers.

### Fallback Method: Email Contact
If you cannot use GitHub Private Vulnerability Reporting or wish to contact the maintainers directly, please email:
**[pablodiegoo@gmail.com](mailto:pablodiegoo@gmail.com)**

Please include:
- Subject prefix: `[SECURITY] PerGo Vulnerability Report: <Brief Description>`
- A clear description of the issue and affected versions or channels.
- Step-by-step reproduction instructions or PoC.
- Impact assessment.

### If a Secret Was Leaked:
1. **Rotate Immediately:** The first and most critical action is to immediately rotate/revoke the leaked credential in the provider dashboard (Meta, Stripe, AWS, database, etc.). Deleting the file in a new git commit does **not** protect a compromised credential because it remains accessible in git history.
2. **Report Privately:** Follow the private disclosure channels above. Do **not** open a public issue.

---

## 8. Response & Disclosure Process

1. **Acknowledgment**: We aim to acknowledge receipt of security reports within **48 hours**.
2. **Investigation & Triage**: We will investigate and confirm the report within **5 business days**, providing an estimated timeline for a patch.
3. **Remediation**: Once verified, a fix will be prepared and tested across supported branches in a private repository branch.
4. **Coordinated Release & Advisory**: We will publish a patch release along with a public GitHub Security Advisory crediting the reporter (unless anonymity is requested).
