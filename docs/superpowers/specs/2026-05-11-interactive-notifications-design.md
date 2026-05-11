# Interactive Notifications Design

## 1. Overview
The current `notify.ask` MCP tool is hardcoded to "Approve/Reject" flows via the `discord.approval.request` and `webex.approval.request` integration endpoints. This design extends the interactive notification system to support arbitrary multiple-choice options and optional custom text/feedback inputs.

## 2. API Changes

### 2.1 Core `notify.ask` Tool
- **Current Signature:** Takes `question` (string).
- **New Signature:**
  - `question` (string, required): The prompt to show the user.
  - `options` (array of strings, optional): The specific choices available to the user. Defaults to `["Approve", "Reject"]` if omitted.
  - `allow_text` (boolean, optional): Whether to provide a UI affordance for custom text feedback. Defaults to `false`.
- **Return Format:**
  - The tool will now return a structured JSON object to the orchestrator:
    ```json
    {
      "selection": "Option Name", // or "custom" if text was submitted
      "custom_text": "..." // omitted or empty if a standard button was clicked
    }
    ```

### 2.2 Integration APIs
- The integration MCP tools `approval.request` will be renamed to `interactive.request` for both Discord and Webex.
- Their arguments will map identically to the `notify.ask` signature (`title`, `description`, `options`, `allow_text`, `timeout_s`).
- The internal routing structs `ApprovalDecision` will be renamed to `InteractiveDecision`, featuring `Selection` and `CustomText` fields.

## 3. Implementation Details

### 3.1 Discord Integration
- **Button Generation:** Discord permits up to 5 buttons per `ActionRow`, and up to 5 rows per message. We will paginate the `options` array across multiple rows if necessary.
- **Custom Text Input:** If `allow_text` is `true`, a final button (e.g., "📝 Feedback / Other...") will be appended.
- **Modal Interaction:** Clicking the feedback button will trigger a `discordgo.InteractionResponseModal` instead of an immediate interaction acknowledgement. This modal will contain a text input field.
- **Webhook Handling:** The `onInteractionCreate` webhook handler will be updated to distinguish between standard `InteractionMessageComponent` (button clicks) and `InteractionModalSubmit` (modal submissions). Both will resolve the pending `requestID` in the router.

### 3.2 Webex Integration
- **Adaptive Card Changes:** The bot will dynamically populate `Action.Submit` actions for each item in the `options` array.
- **Custom Text Input:** If `allow_text` is `true`, an `Action.ShowCard` button will be included. Clicking this expands a sub-card containing an `Input.Text` element and a dedicated "Submit Feedback" `Action.Submit`.
- **Action Handling:** When the user submits, the `AttachmentAction` payload will contain the `decision` (from a standard button) or `custom_text` (from the input field). This data will be routed to resolve the `requestID`.

## 4. Scope and Consistency
- **Scope:** This design is scoped entirely to the notification abstractions and their immediate Discord/Webex adapters. It does not dictate how LLMs will utilize the returned values.
- **Error Handling:** Timeouts will continue to return a `{ "selection": "timeout" }` payload. Unrecognized or malformed responses from the APIs will log errors and resolve the interaction accordingly.