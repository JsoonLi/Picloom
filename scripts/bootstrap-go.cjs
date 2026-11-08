// Portable, checksum-verified development toolchain; never installs system-wide.
const fs = require('node:fs/promises');
const path = require('node:path');
const crypto = require('node:crypto');
const { execFileSync } = require('node:child_process');
const version = '1.27.3';
async function main() {
  if (process.platform !== 'win32' || process.arch !== 'x64') throw new Error('此引导脚本仅用于 Windows x64；其他平台请安装 Go 1.27.3。');
  const directory = path.resolve(__dirname, '../.cache/toolchains');
  const executable = path.join(directory, 'go/bin/go.exe');
  try { if (execFileSync(executable, ['version'], { encoding: 'utf8', windowsHide: true }).includes(`go${version} `)) return console.log(executable); } catch {}
  const response = await fetch(`https://go.dev/dl/go${version}.windows-amd64.zip`, { signal: AbortSignal.timeout(180000) });
  if (!response.ok) throw new Error(`Go 下载失败 (${response.status})`);
  const bytes = Buffer.from(await response.arrayBuffer());
  if (crypto.createHash('sha256').update(bytes).digest('hex') !== 'f26081f438319757f8a1b46d7d0b2c5ec7583d4038e485e0aa725cfde7885fff') throw new Error('Go 下载校验失败。');
  await fs.mkdir(directory, { recursive: true });
  const archive = path.join(directory, `go${version}.zip`);
  await fs.writeFile(archive, bytes);
  const quote = (value) => `'${value.replaceAll("'", "''")}'`;
  execFileSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', `Expand-Archive -LiteralPath ${quote(archive)} -DestinationPath ${quote(directory)} -Force`], { windowsHide: true, stdio: 'inherit' });
  console.log(execFileSync(executable, ['version'], { encoding: 'utf8', windowsHide: true }).trim());
  console.log(executable);
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
