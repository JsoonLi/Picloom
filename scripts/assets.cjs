const { Resvg } = require("@resvg/resvg-js");
const fs = require("node:fs");
const path = require("node:path");
const root = path.resolve(__dirname, "..");
const svg = fs.readFileSync(path.join(root, "public/icon.svg"), "utf8");
const png = new Resvg(svg, { fitTo: { mode: "width", value: 256 } })
  .render()
  .asPng();
fs.mkdirSync(path.join(root, "build"), { recursive: true });
fs.writeFileSync(path.join(root, "build/icon.png"), png);
const macPng = new Resvg(svg, { fitTo: { mode: "width", value: 1024 } })
  .render()
  .asPng();
fs.writeFileSync(path.join(root, "build/icon-mac.png"), macPng);
const header = Buffer.alloc(22);
header.writeUInt16LE(1, 2);
header.writeUInt16LE(1, 4);
header.writeUInt16LE(1, 10);
header.writeUInt16LE(32, 12);
header.writeUInt32LE(png.length, 14);
header.writeUInt32LE(22, 18);
fs.writeFileSync(
  path.join(root, "build/icon.ico"),
  Buffer.concat([header, png])
);
console.log("Rendered Windows PNG/ICO and macOS PNG from public/icon.svg");
