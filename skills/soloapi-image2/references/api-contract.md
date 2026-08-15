# SoloAPI Image2 API contract

The helper uses OpenAI-compatible synchronous image endpoints beneath the built-in SoloAPI address `https://api.soloapi.cc/v1`. `SOLOAPI_IMAGE2_BASE_URL` is an optional developer/testing override and is not part of end-user setup.

## Generate

- `POST /images/generations`
- `Authorization: Bearer <local environment key>`
- `Content-Type: application/json`
- Required body: `model=gpt-image-2`, `prompt`, `n=1`
- Optional body: `size`

## Edit

- `POST /images/edits`
- `Authorization: Bearer <local environment key>`
- `Content-Type: multipart/form-data`
- Required fields: `model=gpt-image-2`, `prompt`, `n=1`, one to four repeated `image` parts
- Optional field: `size`
- Each reference image is limited to 15 MiB, keeping four-image requests below the 64 MiB gateway limit.

## Response

The helper accepts the standard `data[0].b64_json` or `data[0].url` shape. URL downloads never forward the API Authorization header. Maximum accepted image response size is 64 MiB.

The helper deliberately makes one network attempt and refuses redirects for paid POST requests. Retrying or redirecting a timed-out non-idempotent image request could cause a second charge.
