# Security Policy

The PerGo project takes the security of our CPaaS omnichannel platform and the data entrusted to it very seriously. We appreciate the efforts of security researchers and community members who report security vulnerabilities responsibly.

---

## Supported Versions

We actively provide security patches and updates for the following versions of PerGo:

| Version | Supported          | Status                                 |
| ------- | ------------------ | -------------------------------------- |
| `main`  | :white_check_mark: | Active development & rolling fixes    |
| `v1.x`  | :white_check_mark: | Latest stable releases                 |
| `< 1.0` | :x:                | End of life; please upgrade to `v1.x`  |

---

## Reporting a Vulnerability

### Preferred Method: GitHub Private Vulnerability Reporting

PerGo uses GitHub's built-in **Private Vulnerability Reporting** feature to coordinate confidential disclosures.

1. Navigate to the [PerGo Security Advisories](https://github.com/pablodiegoo/PerGo/security/advisories/new) page.
2. Click on **Report a vulnerability**.
3. Fill out the report form detailing the vulnerability, impact, affected component or channel, and reproduction steps.
4. Submit the report to open a private advisory channel directly with the maintainers.

### Fallback Method: Email Contact

If you cannot use GitHub Private Vulnerability Reporting or wish to contact the maintainers directly, please email:

**[pablodiegoo@gmail.com](mailto:pablodiegoo@gmail.com)**

Please include:
- Subject prefix: `[SECURITY] PerGo Vulnerability Report: <Brief Description>`
- A clear description of the issue and affected versions or channels (e.g., WhatsApp Web, WABA, Telegram, Webhook router).
- Step-by-step instructions or proof-of-concept (PoC) code to reproduce the issue.
- Impact assessment (e.g., unauthorized access, denial of service, privilege escalation, data leakage).
- Any potential remediations or patches you have identified.

---

## Response & Disclosure Process

1. **Acknowledgment**: We aim to acknowledge receipt of security reports within **48 hours**.
2. **Investigation & Triage**: We will investigate and confirm the report within **5 business days**, providing an estimated timeline for a patch.
3. **Remediation**: Once verified, a fix will be prepared and tested across supported branches in a private repository branch.
4. **Coordinated Release & Advisory**: We will publish a patch release along with a public GitHub Security Advisory crediting the reporter (unless anonymity is requested).

---

## Guidelines for Responsible Disclosure

To protect PerGo users:
- Do not disclose security vulnerabilities publicly or share them with third parties until a fix has been released and an advisory published.
- Do not exploit identified issues beyond what is necessary to demonstrate proof-of-concept.
- Do not compromise user data or disrupt PerGo infrastructure.
