# vite-plugin-loca

Expose a Vite dev server through Loca at a trusted HTTPS `.localhost` hostname.

```ts
import { defineConfig } from "vite";
import loca from "vite-plugin-loca";

export default defineConfig({
	plugins: [loca("app.project.localhost")],
});
```

For Vite development, the plugin checks `http://localhost/_loca/v1/health` for at most 150 ms. If Loca is available, the plugin chooses a loopback port, registers the actual Vite port once the server is listening, and attempts an owner-checked cleanup when the server closes. If Loca is unavailable, Vite continues with its normal local URL. Pass `{ required: true }` to fail Vite startup instead:

```ts
plugins: [loca("app.project.localhost", { required: true })],
```

Release builds do not require or contact Loca.

Loca must be installed, running, and trusted on macOS. The hostname must end in `.localhost`; nested names such as `app.project.localhost` are supported.

## Local development

From this repository, pack and install the package locally without publishing it:

```sh
npm pack ./packages/vite-plugin-loca
npm install --save-dev /path/to/loca/vite-plugin-loca-0.1.0.tgz
```
