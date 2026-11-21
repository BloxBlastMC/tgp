import { spawn, type ChildProcess } from 'node:child_process'
import { createHmac } from 'node:crypto'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

export type TestServer = { url: string; setupToken: string; stop: () => Promise<void> }

/** Starts bin/tgp-server on a random port with a fresh data dir. env adds
 * to the server's environment. */
export async function startServer(env: Record<string, string> = {}): Promise<TestServer> {
  const bin = process.env.TGP_SERVER_BIN ?? resolve(import.meta.dirname, '../../bin/tgp-server')
  const port = 40000 + Math.floor(Math.random() * 20000)
  const dataDir = mkdtempSync(join(tmpdir(), 'tgp-e2e-'))
  const proc: ChildProcess = spawn(bin, [], {
    env: { ...process.env, TGP_DATA_DIR: dataDir, TGP_LISTEN: `127.0.0.1:${port}`, TGP_LOG_LEVEL: 'info', ...env },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let output = ''
  const setupToken = await new Promise<string>((res, rej) => {
    const timer = setTimeout(() => rej(new Error(`server didn't start:\n${output}`)), 15_000)
    const onData = (b: Buffer) => {
      output += b.toString()
      const m = output.match(/setup token:[\s│]+([A-Za-z0-9_-]{10,})/)
      if (m && output.includes('tgp-server listening')) {
        clearTimeout(timer)
        res(m[1]!)
      }
    }
    proc.stderr!.on('data', onData)
    proc.stdout!.on('data', onData)
    proc.on('exit', (code) => rej(new Error(`server exited with ${code}:\n${output}`)))
  })
  return {
    url: `http://127.0.0.1:${port}`,
    setupToken,
    stop: () =>
      new Promise((res) => {
        proc.once('exit', () => res())
        proc.kill('SIGTERM')
      }),
  }
}

/** RFC 6238 TOTP (SHA-1, 6 digits, 30 s) for a base32 secret. */
export function totp(secret: string, at = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const c of secret.replace(/=+$/, '').toUpperCase()) bits += alphabet.indexOf(c).toString(2).padStart(5, '0')
  const key = Buffer.alloc(Math.floor(bits.length / 8))
  for (let i = 0; i < key.length; i++) key[i] = parseInt(bits.slice(i * 8, i * 8 + 8), 2)
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)))
  const h = createHmac('sha1', key).update(counter).digest()
  const o = h[h.length - 1]! & 0xf
  const n = ((h[o]! & 0x7f) << 24) | (h[o + 1]! << 16) | (h[o + 2]! << 8) | h[o + 3]!
  return String(n % 1_000_000).padStart(6, '0')
}
