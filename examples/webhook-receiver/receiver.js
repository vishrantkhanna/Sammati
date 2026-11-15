// Receives Sammati webhooks and checks their signature. Node 18 or later.
//
//   SAMMATI_WEBHOOK_SECRET=whsec_... node receiver.js
const http = require("node:http");
const crypto = require("node:crypto");

const secret = process.env.SAMMATI_WEBHOOK_SECRET;
if (!secret) throw new Error("Set SAMMATI_WEBHOOK_SECRET");

function valid(header, body) {
  const m = /^t=(\d+),v1=([0-9a-f]{64})$/.exec(header || "");
  if (!m) return false;
  if (Math.abs(Date.now() / 1000 - Number(m[1])) > 300) return false; // older than five minutes
  const want = crypto.createHmac("sha256", secret).update(`${m[1]}.`).update(body).digest();
  return crypto.timingSafeEqual(want, Buffer.from(m[2], "hex"));
}

http.createServer((req, res) => {
  const chunks = [];
  req.on("data", (c) => chunks.push(c));
  req.on("end", () => {
    const body = Buffer.concat(chunks); // the exact bytes, before any parsing
    if (!valid(req.headers["x-sammati-signature"], body)) {
      res.writeHead(401).end();
      return;
    }
    const event = JSON.parse(body);
    for (const r of event.records) {
      // Update your own system here, for example unsubscribe on withdraw.
      console.log(event.event, event.subject.slice(0, 10), r.purpose);
    }
    res.writeHead(204).end();
  });
}).listen(3000, () => console.log("Listening on :3000"));
