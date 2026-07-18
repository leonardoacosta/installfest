/**
 * runBun.ts — spawns a Bun runner script as a fresh subprocess with an env
 * overlay, capturing stdout/stderr/exit code. See run-open-items.ts's
 * header comment for why the tests run these as subprocesses instead of
 * importing modules in-process.
 */
import { join } from "node:path";

export interface BunRunResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

export async function runBunScript(
  scriptName: string,
  args: string[],
  env: Record<string, string>,
): Promise<BunRunResult> {
  const scriptPath = join(import.meta.dir, "..", "runners", scriptName);
  const proc = Bun.spawn([process.execPath, "run", scriptPath, ...args], {
    env: { ...process.env, ...env },
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr, exitCode] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  return { stdout, stderr, exitCode };
}
