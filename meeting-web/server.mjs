import { createReadStream, existsSync } from "node:fs";
import { extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:http";

const root = join(fileURLToPath(new URL(".", import.meta.url)), "dist");
const port = Number(process.env.MEETING_WEB_PORT || 5173);

const contentTypes = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".webp": "image/webp",
  ".ico": "image/x-icon"
};

createServer((request, response) => {
  const rawPath = decodeURIComponent(new URL(request.url, `http://${request.headers.host}`).pathname);
  const requested = normalize(rawPath === "/" ? "/index.html" : rawPath);
  let filePath = join(root, requested);

  if (!filePath.startsWith(root) || !existsSync(filePath)) {
    filePath = join(root, "index.html");
  }

  response.setHeader("Content-Type", contentTypes[extname(filePath)] || "application/octet-stream");
  createReadStream(filePath)
    .on("error", () => {
      response.writeHead(404);
      response.end("not found");
    })
    .pipe(response);
}).listen(port, "0.0.0.0", () => {
  console.log(`meeting-web listening on http://localhost:${port}`);
});
