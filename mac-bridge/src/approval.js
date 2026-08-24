import { request, retryRequest } from "./client.js";

export const unsupportedApprovalMessage = "Sensitive operation requires explicit user approval, but the current MCP client does not support interactive approval.";

export class ApprovalFlowError extends Error {
  constructor(code, message, ticket = {}, extra = {}) {
    super(message);
    this.name = "ApprovalFlowError";
    this.code = code;
    this.isApprovalFlowError = true;
    this.details = { ...ticket, ...extra, approval_status: code, message };
  }
}

export function isApprovalRequired(error) {
  return error?.code === "approval_required" && Boolean(error?.details?.approval_id) && Boolean(error?.requestSpec);
}

export async function handleApprovalRequired({ server, error }) {
  const ticket = normalizeTicket(error.details);
  if (!supportsFormElicitation(server)) {
    throw new ApprovalFlowError("approval_interaction_unavailable", unsupportedApprovalMessage, ticket);
  }

  let decision;
  try {
    decision = await requestUserApproval(server, ticket);
  } catch (approvalError) {
    if (approvalError instanceof ApprovalFlowError) throw approvalError;
    throw new ApprovalFlowError("approval_interaction_failed", "Could not obtain explicit user approval; the operation was not executed.", ticket);
  }

  let decisionResult;
  try {
    decisionResult = await request(
      "POST",
      `/v1/approvals/${encodeURIComponent(ticket.approval_id)}/decision`,
      { decision }
    );
  } catch (decisionError) {
    throw new ApprovalFlowError(
      decisionError?.code || "approval_decision_failed",
      "The approval decision could not be recorded; the operation was not executed.",
      ticket,
      { upstream_code: decisionError?.code }
    );
  }

  const expectedState = decision === "approve" ? "approved" : "denied";
  if (decisionResult?.state !== expectedState) {
    throw new ApprovalFlowError(
      "approval_decision_unconfirmed",
      "The approval decision was not confirmed by the NAS; the operation was not executed.",
      { ...ticket, state: decisionResult?.state },
      { upstream_code: "unexpected_approval_state" }
    );
  }

  if (decision === "deny") {
    throw new ApprovalFlowError("approval_denied", "User denied this sensitive operation.", { ...ticket, state: "denied" });
  }

  try {
    // This is the only automatic retry in the approval flow. The prepared
    // request preserves the exact method, path, serialized body, request ID,
    // idempotency key, and timeout policy from the initial attempt.
    return await retryRequest(error.requestSpec, ticket.approval_id);
  } catch (retryError) {
    throw new ApprovalFlowError(
      retryError?.code || "approval_retry_failed",
      retryMessage(retryError?.code),
      { ...ticket, state: stateFor(retryError?.code) },
      { upstream_code: retryError?.code }
    );
  }
}

export async function requestUserApproval(server, ticket) {
  if (!supportsFormElicitation(server)) {
    throw new ApprovalFlowError("approval_interaction_unavailable", unsupportedApprovalMessage, ticket);
  }
  const result = await server.server.elicitInput({
    mode: "form",
    message: approvalMessage(ticket),
    // The decision field is optional on purpose. A client such as Hermes may
    // return action=accept with content={}, which means approve once for this
    // simple two-choice approval request.
    requestedSchema: {
      type: "object",
      properties: {
        decision: {
          type: "string",
          title: "Decision",
          description: "Choose approve once or deny.",
          enum: ["approve", "deny"]
        }
      }
    }
  });
  return decisionFromResult(result, ticket);
}

export function supportsFormElicitation(server) {
  const protocol = server?.server;
  const capabilities = protocol?.getClientCapabilities?.();
  return typeof protocol?.elicitInput === "function" && capabilities?.elicitation?.form !== undefined;
}

function decisionFromResult(result, ticket) {
  const content = result?.content;
  const explicit = typeof content?.decision === "string" ? content.decision.toLowerCase() : undefined;
  if (explicit === "approve" || explicit === "approved") return "approve";
  if (explicit === "deny" || explicit === "denied") return "deny";
  if (typeof content?.approved === "boolean") return content.approved ? "approve" : "deny";
  if (result?.action === "accept") return "approve";
  if (result?.action === "decline" || result?.action === "cancel") return "deny";
  throw new ApprovalFlowError("approval_invalid_response", "The client returned an invalid approval response; the operation was not executed.", ticket);
}

function normalizeTicket(value = {}) {
  return {
    approval_id: value.approval_id,
    request_id: value.request_id,
    operation: value.operation,
    risk: value.risk,
    target: value.target,
    summary: value.summary,
    state: value.state,
    created_at: value.created_at,
    expires_at: value.expires_at
  };
}

function approvalMessage(ticket) {
  const lines = [
    "QNAP AI Control requests approval for a sensitive operation.",
    `Operation: ${ticket.operation || "unknown"}`,
    `Target: ${ticket.target || "unspecified"}`,
    `Risk: ${ticket.risk || "sensitive"}`,
    `Summary: ${ticket.summary || "Sensitive operation"}`
  ];
  if (ticket.expires_at) lines.push(`Expires: ${ticket.expires_at}`);
  lines.push("Choose approve once to execute this exact request, or deny to stop it.");
  return lines.join("\n");
}

function retryMessage(code) {
  switch (code) {
    case "approval_expired": return "Approval expired before the operation could run. Start the operation again.";
    case "approval_used": return "This approval was already used; the operation was not retried.";
    case "approval_mismatch": return "The approval did not match the original request; the operation was not retried.";
    case "approval_not_approved": return "The approval was not recorded as approved; the operation was not executed.";
    case "approval_denied": return "User denied this sensitive operation.";
    default: return "The approved operation could not be retried; the operation was not automatically repeated.";
  }
}

function stateFor(code) {
  if (code === "approval_expired") return "expired";
  if (code === "approval_used") return "used";
  if (code === "approval_denied") return "denied";
  return undefined;
}
