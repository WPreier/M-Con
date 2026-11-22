const token = document.querySelector("#token");
const out = document.querySelector("#out");
const form = document.querySelector("#mail");
let current = "";

token.value = sessionStorage.getItem("mcon-token") || "";
token.addEventListener("change", () => sessionStorage.setItem("mcon-token", token.value));

function headers() {
  return { Authorization: "Bearer " + token.value, "Content-Type": "application/json" };
}

function list(value) {
  return value.split(",").map((s) => s.trim()).filter(Boolean);
}

form.addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const data = new FormData(form);
  const body = {
    from: data.get("from"),
    to: list(data.get("to")),
    cc: list(data.get("cc") || ""),
    bcc: list(data.get("bcc") || ""),
    reply_to: data.get("reply_to") || "",
    subject: data.get("subject"),
    text: data.get("text") || "",
    html: data.get("html") || "",
    provider: data.get("provider") || "",
    tags: list(data.get("tags") || ""),
    headers: {},
    spam: { hint: data.get("spam_hint") || "" }
  };
  if (data.get("header_name")) body.headers[data.get("header_name")] = data.get("header_value") || "";
  const meta = (data.get("metadata") || "").trim();
  if (meta) body.metadata = JSON.parse(meta);
  const file = data.get("attachment");
  if (file && file.size) {
    const buf = await file.arrayBuffer();
    const bytes = new Uint8Array(buf);
    let bin = "";
    bytes.forEach((b) => { bin += String.fromCharCode(b); });
    body.attachments = [{ filename: file.name, content_type: file.type || "application/octet-stream", content_base64: btoa(bin) }];
  }
  const res = await fetch("/admin/v1/test/send", { method: "POST", headers: headers(), body: JSON.stringify(body) });
  const json = await res.json();
  current = json.id || "";
  out.textContent = JSON.stringify(json, null, 2);
  if (current) poll();
});

async function poll() {
  if (!current) return;
  const res = await fetch("/admin/v1/messages/" + current, { headers: headers() });
  const json = await res.json();
  out.textContent = JSON.stringify(json, null, 2);
  if (!["sent", "blocked", "dead", "cancelled"].includes(json.state)) {
    setTimeout(poll, 800);
  }
}

document.querySelector("#resend").addEventListener("click", async () => {
  if (!current) return;
  const res = await fetch("/admin/v1/messages/" + current + "/resend", {
    method: "POST", headers: headers(), body: JSON.stringify({ skip_spam: document.querySelector("#skip").checked })
  });
  const json = await res.json();
  current = json.id || current;
  out.textContent = JSON.stringify(json, null, 2);
  poll();
});

document.querySelector("#replay").addEventListener("click", async () => {
  if (!current) return;
  const res = await fetch("/admin/v1/messages/" + current + "/replay", { method: "POST", headers: headers() });
  out.textContent = JSON.stringify(await res.json(), null, 2);
  poll();
});
