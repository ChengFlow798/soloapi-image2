# SoloAPI Image2

让 Codex 在保留现有文本主模型的同时，通过 SoloAPI 调用 `gpt-image-2` 完成文生图、参考图生成和图片编辑。

## 安装

把下面这句话完整发给 Codex：

```text
请从 https://github.com/ChengFlow798/soloapi-image2/tree/main/skills/soloapi-image2 安装 soloapi-image2 Skill。
```

安装完成后，把 SoloAPI API Key 发给 Codex：

```text
请帮我配置 soloapi-image2：
API Key：粘贴你的 SoloAPI API Key
```

接口地址已经内置，不需要填写。配置完成后，完全退出并重新打开 Codex。

## 使用

文生图：

```text
使用 $soloapi-image2 生成一张图片：雨后的未来城市，电影感广角，蓝绿色霓虹。
```

参考图编辑：

```text
使用 $soloapi-image2，参考我附上的图片，保留构图和配色，改成纸雕风格。
```

## 能做什么

- 固定调用 SoloAPI 的 `gpt-image-2`，每次生成 1 张图片。
- 支持文生图和图片编辑。
- 支持最多 4 张参考图，每张不超过 15 MiB。
- 支持 Windows x64/Arm64、macOS Intel/Apple Silicon、Linux x64/Arm64。
- 真实付费请求必须由 Skill 显式确认，失败时不会自动重试付费请求。

## 本地验证

仓库包含 Go 单元测试和 Windows 黑盒测试。黑盒测试使用 localhost mock，不会请求真实上游，也不会产生费用。

从源码重新编译全部平台版本：

```bash
docker run --rm -v "$PWD/skills/soloapi-image2:/work" -w /work golang:1.24.5-bookworm sh /work/scripts/build-all.sh
```

## 许可证

本项目基于 `fengfengzhidao/codex-image2-skill` 的工作流与概念重新实现，并保留上游 MIT License 与署名。详见 [LICENSE](skills/soloapi-image2/LICENSE) 和 [NOTICE](skills/soloapi-image2/NOTICE)。
