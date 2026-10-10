import { randomBytes } from "node:crypto";
import { readFile } from "node:fs/promises";
import { createServer } from "node:net";
import { homedir } from "node:os";
import { join } from "node:path";

const healthPath = "/_loca/v1/health";
const servicesPath = "/_loca/v1/dev-services";

const colorsEnabled =
	!(process.env.NO_COLOR || process.argv.includes("--no-color")) &&
	(!!process.env.FORCE_COLOR ||
		process.argv.includes("--color") ||
		process.platform === "win32" ||
		(process.stdout.isTTY && process.env.TERM !== "dumb") ||
		!!process.env.CI);

function paint(open, close, value) {
	const text = String(value);
	return colorsEnabled ? `${open}${text}${close}` : text;
}

function green(value) {
	return paint("\u001b[32m", "\u001b[39m", value);
}

function bold(value) {
	return paint("\u001b[1m", "\u001b[22m", value);
}

function cyan(value) {
	return paint("\u001b[36m", "\u001b[39m", value);
}

function colorUrl(url) {
	return cyan(url.replace(/:(\d+)\//, (_, port) => `:${bold(port)}/`));
}

function normalizeHostname(value) {
	if (typeof value !== "string" || !value.toLowerCase().endsWith(".localhost")) {
		throw new TypeError('Loca hostname must end in ".localhost"');
	}
	const hostname = value.toLowerCase();
	const name = hostname.slice(0, -".localhost".length);
	if (name.length > 253 || name.split(".").some(label => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))) {
		throw new TypeError(`Invalid Loca hostname: ${value}`);
	}
	return hostname;
}

function getDataDir(options) {
	return options.dataDir ?? process.env.LOCA_DATA_DIR ?? join(homedir(), "Library", "Application Support", "loca");
}

async function readToken(options) {
	try {
		return (await readFile(join(getDataDir(options), "control.token"), "utf8")).trim();
	} catch {
		return "";
	}
}

async function detectLoca(options) {
	const token = await readToken(options);
	if (!token) return undefined;
	const controlUrl = (options.controlUrl ?? process.env.LOCA_CONTROL_URL ?? "http://localhost").replace(/\/+$/, "");
	const timeout = AbortSignal.timeout(options.timeoutMs ?? 150);
	try {
		const response = await fetch(`${controlUrl}${healthPath}`, { signal: timeout });
		if (!response.ok) return undefined;
		const info = await response.json();
		if (info.service !== "loca" || info.apiVersion !== 1) return undefined;
		return { token, controlUrl };
	} catch {
		return undefined;
	}
}

function reserveLoopbackPort() {
	return new Promise((resolve, reject) => {
		const listener = createServer();
		listener.once("error", reject);
		listener.listen(0, "127.0.0.1", () => {
			const address = listener.address();
			if (!address || typeof address === "string") {
				listener.close(() => reject(new Error("Could not allocate a Loca port")));
				return;
			}
			const { port } = address;
			listener.close(error => (error ? reject(error) : resolve(port)));
		});
	});
}

async function register(server, hostname, options, detected) {
	if (!server.httpServer || !detected) return;
	const address = server.httpServer.address();
	if (!address || typeof address === "string") return;
	const owner = randomBytes(16).toString("hex");
	let registration;
	let serverClosed = false;
	const cleanup = () => {
		if (!registration) {
			serverClosed = true;
			return;
		}
		fetch(`${detected.controlUrl}${servicesPath}`, {
			method: "DELETE",
			headers: {
				Authorization: `Bearer ${detected.token}`,
				"Content-Type": "application/json",
			},
			body: JSON.stringify({ hostname, owner: registration.owner }),
			signal: AbortSignal.timeout(options.timeoutMs ?? 150),
		})
			.then(response => {
				if (!response.ok) throw new Error(`Loca returned HTTP ${response.status}`);
			})
			.catch(error => {
				server.config.logger.warn(`Loca: could not clean up ${hostname}: ${error.message}`);
			});
	};
	server.httpServer.once("close", cleanup);
	try {
		const response = await fetch(`${detected.controlUrl}${servicesPath}`, {
			method: "POST",
			headers: {
				Authorization: `Bearer ${detected.token}`,
				"Content-Type": "application/json",
			},
			body: JSON.stringify({ hostname, port: address.port }),
			signal: AbortSignal.timeout(options.timeoutMs ?? 150),
		});
		if (!response.ok) throw new Error(`Loca returned HTTP ${response.status}`);
		registration = await response.json();
		if (registration.hostname !== hostname || registration.port !== address.port || !registration.owner) {
			throw new Error("Loca returned an invalid registration response");
		}
		server.config.logger.info(`  ${green("➜")}  ${bold("Local")}:   ${colorUrl(`https://${hostname}/`)}`);
		if (serverClosed) cleanup();
	} catch (error) {
		server.config.logger.warn(`Loca: could not register ${hostname}: ${error.message}`);
	}
}

/** @param {string} hostname @param {import('./index.d.ts').LocaOptions} [options] */
export default function loca(hostname, options = {}) {
	const normalizedHostname = normalizeHostname(hostname);
	let detected;
	let selectedPort;

	return {
		name: "vite-plugin-loca",
		async config(config, environment) {
			if (environment.command !== "serve" || config.server?.middlewareMode) return;
			detected = await detectLoca(options);
			if (!detected) {
				if (!options.required) return;
				throw new Error(
					`Loca is required to serve ${normalizedHostname}. Start Loca and make sure its control API is available at ${options.controlUrl ?? process.env.LOCA_CONTROL_URL ?? "http://localhost"}.`,
				);
			}
			selectedPort = await reserveLoopbackPort();
			return {
				server: {
					host: "127.0.0.1",
					port: selectedPort,
					strictPort: false,
					https: false,
				},
			};
		},
		configureServer(server) {
			if (!detected || selectedPort === undefined) return;
			server.httpServer?.once("listening", () => void register(server, normalizedHostname, options, detected));
		},
		configurePreviewServer(server) {
			if (!detected || selectedPort === undefined) return;
			server.httpServer?.once("listening", () => void register(server, normalizedHostname, options, detected));
		},
	};
}
