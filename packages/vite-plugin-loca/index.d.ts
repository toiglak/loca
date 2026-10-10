import type { Plugin } from "vite";

export interface LocaOptions {
	/** Fail Vite startup when Loca is unavailable instead of using Vite's normal local URL. */
	required?: boolean;
	/** Override the directory containing Loca's control token. */
	dataDir?: string;
	/** Override the local control API URL. Useful for isolated tests. */
	controlUrl?: string;
	/** Maximum wait for Loca discovery. Defaults to 150 milliseconds. */
	timeoutMs?: number;
}

/** Register a Vite dev server under a Loca-managed `.localhost` hostname. */
export default function loca(hostname: string, options?: LocaOptions): Plugin;
