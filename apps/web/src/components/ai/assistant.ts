import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";

export type Conversation = components["schemas"]["AIConversation"];
export type Turn = components["schemas"]["AITurn"];
export type Fact = components["schemas"]["AIFact"];
export type TurnContext = components["schemas"]["AITurnContext"];
export type TurnStatus = components["schemas"]["AITurnStatus"];

// An event of a turn's stream (askAIConversation): a status while the assistant reads the books, then the turn as kept,
// or the problem that stopped it.
export type TurnEvent = { kind: "status"; status: TurnStatus } | { kind: "answer"; turn: Turn } | { kind: "error"; problem: Record<string, unknown> };

// startConversation starts one of the person's own conversations with the assistant, titled by its first question.
export async function startConversation(title: string): Promise<Conversation> {
  const { data, error } = await api.POST("/v1/assistant/conversations", { params: { header: { "Idempotency-Key": newKey() } }, body: { title } });
  if (error) throw error;
  return data;
}

// ask asks a question in a conversation and hands each event of its stream to on, in order, as it arrives.
export async function ask(conversation: string, question: string, context: TurnContext | undefined, on: (e: TurnEvent) => void): Promise<void> {
  const { data, error } = await api.POST("/v1/assistant/conversations/{conversation_id}/turns", {
    params: { path: { conversation_id: conversation } },
    body: context ? { question, context } : { question },
    parseAs: "stream",
  });
  if (error) throw error;
  if (!data) return;
  const reader = (data as ReadableStream<Uint8Array>).getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (value) buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, "\n");
    let end = buffer.indexOf("\n\n");
    while (end >= 0) {
      const e = eventOf(buffer.slice(0, end));
      if (e) on(e);
      buffer = buffer.slice(end + 2);
      end = buffer.indexOf("\n\n");
    }
    if (done) break;
  }
  const last = eventOf(buffer);
  if (last) on(last);
}

// eventOf reads one server-sent event; nothing for a comment or a block with no data.
export function eventOf(block: string): TurnEvent | undefined {
  let kind = "message";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) kind = line.slice(6).trim();
    else if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
  }
  if (data.length === 0) return undefined;
  let body: unknown;
  try {
    body = JSON.parse(data.join("\n"));
  } catch {
    return undefined;
  }
  switch (kind) {
    case "status":
      return { kind, status: body as TurnStatus };
    case "answer":
      return { kind, turn: body as Turn };
    case "error":
      return { kind, problem: body as Record<string, unknown> };
  }
  return undefined;
}

// routeOf is the page a fact's lineage opens in the web app (AI-68): the report, the ledger or the record it was read
// from; nothing when the web app has no page for it, and the chip then names its source without a link.
export function routeOf(lineage: string): string | undefined {
  const [path = "", query = ""] = lineage.split("?");
  const statement = /^\/v1\/reports\/statements\/(pl|bs|cf)$/.exec(path);
  if (statement) return `/reports/statements/${statement[1]}`;
  const record = /^\/v1\/(invoices|bills|journals|contacts)\/([0-9a-f-]{36})$/.exec(path);
  if (record) {
    const base = { invoices: "/sales/invoices", bills: "/purchases/bills", journals: "/accounting/journals", contacts: "/contacts" }[record[1] as "invoices"];
    return `${base}/${record[2]}`;
  }
  switch (path) {
    case "/v1/reports/general-ledger":
      return "/reports/general-ledger";
    case "/v1/kpis":
      return "/reports/kpis";
    case "/v1/receivables":
      return "/sales/invoices";
    case "/v1/payables":
      return "/purchases/bills";
    case "/v1/checks":
      return "/checks";
    case "/v1/search":
      return `/search${query ? `?${query}` : ""}`;
  }
  return undefined;
}
