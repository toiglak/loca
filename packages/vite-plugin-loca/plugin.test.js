import assert from "node:assert/strict";
import { once } from "node:events";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import loca from "./index.js";

test("hostname must be a .localhost name and supports nested labels", () => {
	assert.throws(() => loca("localhost"), /must end in/);
	assert.throws(() => loca("project.localhost.example.com"), /must end in/);
	assert.throws(() => loca("bad..project.localhost"), /Invalid Loca hostname/);
	assert.equal(loca("Map.Project.localhost").name, "vite-plugin-loca");
});

test("falls back when Loca is unavailable and leaves release builds unchanged", async () => {
	const dataDir = await mkdtemp(join(tmpdir(), "loca-plugin-"));
	try {
		const plugin = loca("app.localhost", { dataDir });
		assert.equal(await plugin.config({}, { command: "serve" }), undefined);
		assert.equal(await plugin.config({}, { command: "build" }), undefined);
	} finally {
		await rm(dataDir, { recursive: true, force: true });
	}
});

test("can require Loca for dev servers", async () => {
	const dataDir = await mkdtemp(join(tmpdir(), "loca-plugin-"));
	try {
		const plugin = loca("app.localhost", { dataDir, required: true });
		await assert.rejects(plugin.config({}, { command: "serve" }), /Loca is required/);
	} finally {
		await rm(dataDir, { recursive: true, force: true });
	}
});

test("detects Loca quickly, registers the bound Vite port and cleans up its lease", async () => {
	const dataDir = await mkdtemp(join(tmpdir(), "loca-plugin-"));
	const controlServer = createServer(async (request, response) => {
		const chunks = [];
		for await (const chunk of request) chunks.push(chunk);
		const body = Buffer.concat(chunks).toString();
		response.setHeader("Content-Type", "application/json");
		if (request.url === "/_loca/v1/health") {
			response.end(JSON.stringify({ service: "loca", apiVersion: 1 }));
		} else if (request.method === "POST") {
			const registration = JSON.parse(body);
			registeredResolve({ ...registration, headers: request.headers });
			response.statusCode = 201;
			response.end(JSON.stringify({ ...registration, hostname: "app.project.localhost", owner: "test-owner" }));
		} else {
			unregisteredResolve(JSON.parse(body));
			response.end(JSON.stringify({ removed: true }));
		}
	});
	let registeredResolve;
	let unregisteredResolve;
	const registered = new Promise(resolve => (registeredResolve = resolve));
	const unregistered = new Promise(resolve => (unregisteredResolve = resolve));
	const infoMessages = [];
	let infoResolve;
	const infoLogged = new Promise(resolve => (infoResolve = resolve));
	try {
		await writeFile(join(dataDir, "control.token"), "test-control-token");
		controlServer.listen(0, "127.0.0.1");
		await once(controlServer, "listening");
		const controlAddress = controlServer.address();
		const plugin = loca("app.project.localhost", {
			dataDir,
			controlUrl: `http://127.0.0.1:${controlAddress.port}`,
			timeoutMs: 500,
		});
		const config = await plugin.config({}, { command: "serve" });
		assert.equal(config.server.host, "127.0.0.1");
		assert.ok(config.server.port > 0);
		const viteServer = createServer((_request, response) => response.end("vite"));
		const fakeVite = {
			httpServer: viteServer,
			config: {
				logger: {
					info(message) {
						infoMessages.push(message);
						infoResolve();
					},
					warn(message) {
						assert.fail(message);
					},
				},
			},
		};
		plugin.configureServer(fakeVite);
		viteServer.listen(config.server.port, config.server.host);
		await once(viteServer, "listening");
		const [registration] = await Promise.all([registered, infoLogged]);
		assert.equal(registration.hostname, "app.project.localhost");
		assert.equal(registration.port, viteServer.address().port);
		assert.equal(registration.headers.authorization, "Bearer test-control-token");
		assert.deepEqual(
			infoMessages.map(message => message.replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "")),
			["  ➜  Local:   https://app.project.localhost/"],
		);
		viteServer.close();
		await once(viteServer, "close");
		assert.deepEqual(await unregistered, { hostname: "app.project.localhost", owner: "test-owner" });
	} finally {
		controlServer.close();
		await rm(dataDir, { recursive: true, force: true });
	}
});
