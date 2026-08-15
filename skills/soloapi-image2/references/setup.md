# SoloAPI Image2：三步使用

## 1. 安装 Skill

把 Skill 的 GitHub 地址发给 Codex，让它安装 `soloapi-image2`。

## 2. 把 API Key 发给 Codex

发送：

```text
请帮我配置 soloapi-image2：
API Key：粘贴你的 SoloAPI API Key
```

接口地址已经内置在 Skill 里，不需要填写。Codex 会把 Key 写入环境变量：

- `SOLOAPI_IMAGE2_API_KEY`

配置完成后，完全退出并重新打开 Codex。

## 3. 开始生图

```text
使用 $soloapi-image2 生成一张图片：雨后的未来城市，电影感广角，蓝绿色霓虹。
```

参考图编辑：

```text
使用 $soloapi-image2，参考我附上的图片，保留构图和配色，改成纸雕风格。
```
