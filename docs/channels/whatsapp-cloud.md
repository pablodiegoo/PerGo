# WhatsApp Cloud API Setup (Meta WABA)

The WhatsApp Cloud API channel integrates directly with Meta's official Graph API for high-volume enterprise messaging, pre-approved marketing and utility templates, and WhatsApp Interactive Flows.

---

## 1. Prerequisites

Before connecting WhatsApp Cloud API to PerGo, you need:
- A [Meta for Developers](https://developers.facebook.com/) account.
- A verified [Meta Business Manager](https://business.facebook.com/) account.
- A clean phone number that is not currently registered on a physical WhatsApp or WhatsApp Business mobile app.

---

## 2. Meta App & Credential Setup

### Step 1: Create a Meta App
1. Go to **Meta for Developers** > **My Apps** > **Create App**.
2. Select **Other** as the use case, then choose **Business** as the app type.
3. Name your application and link your Business Manager portfolio.
4. On the App Dashboard, locate **WhatsApp** under "Add products to your app" and click **Set up**.

### Step 2: Retrieve Phone Number ID & WABA ID
1. In the left-hand sidebar under WhatsApp, navigate to **API Setup**.
2. Note your **Phone number ID** (e.g. `104928374829102`).
3. Note your **WhatsApp Business Account ID (WABA ID)** (e.g. `102938475610293`).

### Step 3: Generate a Permanent System User Token
> [!IMPORTANT]
> The temporary token generated on the API Setup page expires after 24 hours. For production gateways, you must generate a permanent token via a System User:

1. Open **Business Settings** in your Meta Business Manager.
2. Under **Users** > **System users**, click **Add** and create a system user with the **Admin** role.
3. Click **Add assets**, select **Apps**, choose your WhatsApp app, and grant **Full control**.
4. Click **Generate new token**, select your WhatsApp app, and check the following required permissions:
   - `whatsapp_business_messaging`
   - `whatsapp_business_management`
5. Save the generated access token securely.

---

## 3. Inbound Webhook Configuration

To receive delivery receipts (sent, delivered, read), status callbacks, and incoming customer messages:

1. In Meta Developer Portal under **WhatsApp** > **Configuration**, locate the **Webhook** section and click **Edit**.
2. Enter the following settings:
   - **Callback URL:** `https://<YOUR_PERGO_DOMAIN>/webhooks/waba/<YOUR_WORKSPACE_ID>`
   - **Verify Token:** A custom secret passphrase of your choice (e.g. `pergo_webhook_verify_token_2026`).
3. Click **Verify and Save**.
4. Under **Webhook fields**, click **Manage** and subscribe to **`messages`**. This is **mandatory** for delivery status updates and inbound message processing.

---

## 4. Register Connection in PerGo

In PerGo Admin Console or via REST API:

1. Navigate to **Channels** > **WhatsApp Cloud**.
2. Enter:
   - **Phone Number ID**
   - **WABA Account ID**
   - **Permanent Access Token**
   - **Verify Token** (must match the token entered in Step 3.2 above)
3. Click **Save Connection**. PerGo validates credentials against Meta's Graph API and enables the channel.
