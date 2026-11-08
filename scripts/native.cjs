// Optional npm compatibility commands. The application itself needs only Go.
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const portable = path.join(root, '.cache/toolchains/go/bin', process.platform === 'win32' ? 'go.exe' : 'go');
const installed = spawnSync('go', ['version'], { encoding: 'utf8', windowsHide: true });
const go = installed.status === 0 ? 'go' : fs.existsSync(portable) ? portable : null;
if (!go) {
  console.error('未找到 Go 1.27.3。请安装 Go，或在 Windows x64 运行 node --use-env-proxy scripts/bootstrap-go.cjs。');
  process.exit(1);
}
const op = process.argv[2];
const extra = process.argv.slice(3);
const architecture = process.arch === 'x64' ? 'amd64' : process.arch;
const host = `${process.platform === 'win32' ? 'windows' : process.platform}/${architecture}`;
function run(args, environment = {}) {
  const result = spawnSync(go, args, { cwd: root, env: { ...process.env, ...environment }, stdio: 'inherit', windowsHide: true });
  if (result.error) { console.error(`Go 命令执行失败：${result.error.message}`); process.exit(1); }
  if (result.status !== 0) process.exit(result.status || 1);
}
const production = { GOFLAGS: '', MYGO_INSPECTOR: '' };
switch (op) {
  case 'dev': run(['tool', 'mygo', 'dev', ...extra]); break;
  case 'test': run(['test', './...', ...extra]); break;
  case 'vet': run(['vet', './...', ...extra]); break;
  case 'build':
  case 'pack': run(['tool', 'mygo', 'build', '-skip-installer', '-skip-dmg', ...extra], production); break;
  case 'dist': {
    const target = extra.shift() || host;
    if (!['windows/amd64', 'darwin/amd64', 'darwin/arm64'].includes(target)) { console.error('安装包仅支持 windows/amd64、darwin/amd64 和 darwin/arm64。'); process.exit(1); }
    if (target.startsWith('darwin/') && process.platform !== 'darwin') { console.error('macOS DMG 必须在 macOS 上构建。请使用对应架构的 macOS runner。'); process.exit(1); }
    run(['tool', 'mygo', 'build', '-platform', target, ...extra], production);
    run(['run', './cmd/artifacts', '-platform', target], production);
    break;
  }
  case 'smoke': {
    if (!['windows/amd64', 'darwin/amd64', 'darwin/arm64'].includes(host)) { console.error('原生 smoke 当前支持 Windows x64 和 macOS x64/arm64。'); process.exit(1); }
    run(['tool', 'mygo', 'build', '-debug', '-skip-installer', '-skip-dmg', '-o', '.cache/native-e2e'], { GOFLAGS: '-tags=e2e', MYGO_INSPECTOR: '1' });
    const executable = host.startsWith('windows/') ? '.cache/native-e2e/windows-amd64/Image2AI.exe' : `.cache/native-e2e/${host.replace('/', '-')}/Image2AI.app/Contents/MacOS/Image2AI`;
    run(['run', './cmd/smoke', '-app', executable, ...extra], production);
    break;
  }
  default: console.error('未知原生命令。支持 dev、test、vet、build、pack、dist、smoke。'); process.exit(1);
}
