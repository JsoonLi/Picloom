# Picloom · 绘织

[English](README.md) | **简体中文**

Picloom 是用 Go 和 MyGo 构建的原生 AI 图像创作桌面应用，支持 Windows 和 macOS。连接自己的 OpenAI 兼容图像服务后，可以通过提示词、参考图或画面标注生成图片，并在本地管理作品。

[下载最新版本](https://github.com/JsoonLi/Picloom/releases/latest) · [查看所有版本](https://github.com/JsoonLi/Picloom/releases) · [反馈问题](https://github.com/JsoonLi/Picloom/issues)

## 下载与安装

在 Releases 中选择对应平台的安装包：

| 平台 | 安装包 |
| --- | --- |
| Windows x64 | `Picloom-Setup-<版本>-x64.exe` |
| macOS Intel | `Picloom-<版本>-macOS-x64.dmg` |
| macOS Apple Silicon | `Picloom-<版本>-macOS-arm64.dmg` |

macOS 最低支持 12.0。应用内可在绘图设置的更新页检查新版本、设置自动检查和下载。更新包带有签名校验。

## 开始创作

1. 打开绘图设置，在提供商页填写服务的 Base URL 和 API Key。
2. 获取并启用图片模型，按服务支持情况选择分辨率，保存提供商。
3. 在创作区输入提示词，选择模型、画面比例和输出格式，点击开始生成。
4. 需要以图生图时，添加参考图。生成结果会保存在本地作品库中，可预览、收藏、复制或导出。

应用需要自行配置图像服务和密钥，调用费用由服务商收取。可用模型、分辨率和输出格式取决于所连接的服务。

## 功能

- **文生图与参考图生成**：通过文件选择、拖放或剪贴板添加参考图，最多 4 张。参考图支持 PNG、JPEG 和 WebP，每张不超过 20 MB。
- **标注编辑**：在单张原图上用画笔、矩形或圆圈标出位置，再输入修改要求。原图与标注层会保留，方便继续修改。
- **多种输出格式**：可选择 PNG、JPEG、WebP、GIF 或 SVG。GIF 可设置帧数、每帧时长和尺寸。
- **作品库**：按提示词或模型搜索、筛选收藏、查看生成参数，在预览中切换结果图与参考图，并复用作品的提示词和参数。
- **失败任务重做**：恢复提示词、参考图、标注和生成参数到创作区，修改后再提交。
- **提供商与网络设置**：管理多个图像服务，获取模型列表，设置默认分辨率，选择系统代理、直连或手动代理。
- **外观与更新**：支持浅色、深色和跟随系统主题，以及应用内更新。

GIF、SVG 和标注编辑需要服务端支持相应接口。当前发布版本的 GIF 仅支持文生图，标注编辑支持默认格式、PNG、JPEG 和 WebP 输出。

## 服务接口

Picloom 使用 OpenAI 兼容的图像接口，默认配置如下：

| 用途 | 请求 |
| --- | --- |
| 获取模型 | `GET <Base URL>/models` |
| 文生图 | `POST <Base URL>/images/generations` |
| 参考图与标注编辑 | `POST <Base URL>/images/edits` |

Base URL 示例为 `https://api.example.com/v1`，应填写服务商提供的实际地址。图像请求支持自定义生成和编辑端点。GIF 使用 `output_format`、`frame_count` 和 `frame_duration_ms` 参数，标注编辑额外发送透明 PNG 格式的 `markup` 图层，需确认服务兼容。

## 本地数据

为兼容早期 Image2AI 版本，Picloom 沿用 `Image2AI` 数据目录名称：

| 平台 | 默认数据目录 |
| --- | --- |
| Windows | `%APPDATA%/Image2AI` |
| macOS | `~/Library/Application Support/Image2AI` |

目录中包含 `config.json`、作品记录和图片。API Key 保存在本机配置文件中，备份或分享目录时请移除密钥。生成请求会将提示词、参考图及标注发送给所配置的图像服务。

## 从源码运行

需要 Go 1.27.3。应用使用原生界面，日常开发可直接使用 Go 命令：

```sh
git clone https://github.com/JsoonLi/Picloom.git
cd Picloom
go mod download
go tool mygo dev
```

```sh
go test ./...
go vet ./...
go tool mygo build -skip-installer -skip-dmg
```

构建结果位于 `dist-native/`。项目也提供 npm 命令封装，需要 Node.js 24 或更高版本：

```sh
npm ci
npm run dev
npm run test:smoke
npm run dist:win:x64
npm run dist:mac:x64
npm run dist:mac:arm64
```

安装包请在对应平台构建，macOS DMG 需要在 macOS 上生成。正式版本的 Windows x64、macOS Intel 和 Apple Silicon 安装包由 [GitHub Actions](.github/workflows/build.yml) 构建并发布。

## 第三方许可

依赖与原生主题的许可声明见 [licenses/README.md](licenses/README.md)，并随应用一同打包。
