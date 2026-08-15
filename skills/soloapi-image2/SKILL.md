---
name: soloapi-image2
description: Generate or edit images through SoloAPI's GPT Image 2 endpoint. Use when the user invokes $soloapi-image2 for text-to-image, reference-image generation, style transfer, background replacement, or other image edits. If the API key is not configured, ask the user to provide it and configure the required environment variables for them.
---

# SoloAPI Image2

Keep the user's existing text model as the main Codex model. Use this Skill only when an image needs to be generated or edited.

## First-time setup

Run the helper `check` command. Resolve the directory containing this `SKILL.md` as `$skillRoot` and select the matching binary from `scripts/bin/`.

Windows x64 example:

```powershell
$tool = Join-Path $skillRoot "scripts/bin/soloapi-image2-windows-amd64.exe"
& $tool check
```

If `key_configured` is `false`, ask the user to provide their SoloAPI API Key. After they provide it, configure the current Windows user environment:

```powershell
[Environment]::SetEnvironmentVariable("SOLOAPI_IMAGE2_API_KEY", "<用户提供的API Key>", "User")
```

The SoloAPI endpoint is built into the helper; the user does not need to provide or configure an API address.

Tell the user to completely exit and reopen Codex. Do not test image generation until the restarted session can read the new environment variables.

On macOS or Linux, add the API Key to the user's shell profile and ask them to restart Codex:

```bash
export SOLOAPI_IMAGE2_API_KEY="<用户提供的API Key>"
```

## Generate an image

Create a clear image prompt from the user's request, choose a new output path, and run:

```powershell
& $tool generate --prompt $prompt --out $outputPath --yes
```

Use `--size 1024x1024`, `--size 1536x1024`, or `--size 1024x1536` only when the user specifies an aspect ratio. Otherwise omit `--size`.

## Edit using reference images

Inspect the attached image, resolve its local path, and run:

```powershell
& $tool edit --prompt $prompt --image $referencePath --out $outputPath --yes
```

Repeat `--image` for additional references, up to four.

## Return the result

After success, verify the output file and show it using its absolute local path. If the request fails, return the concise error and ask whether the user wants to try again.

Read `references/setup.md` only when the user asks how to install or configure the Skill. Read `references/api-contract.md` only when debugging gateway compatibility.
