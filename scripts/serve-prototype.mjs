// V4.1 静态原型服务器（局域网访问）
// 用法：node scripts/serve-prototype.mjs [端口]   （默认 8091）
// 根目录：docs/v4.1/prototype
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join, normalize, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..', 'docs', 'v4.1', 'prototype')
const port = Number(process.argv[2] || process.env.PORT || 8091)
const types = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
}

createServer(async (req, res) => {
  try {
    let p = decodeURIComponent(new URL(req.url, 'http://x').pathname)
    if (p === '/') p = '/index.html'
    const file = normalize(join(root, p))
    if (!file.startsWith(root)) { res.writeHead(403); return res.end('403') }
    const body = await readFile(file)
    res.writeHead(200, { 'Content-Type': types[extname(file)] || 'application/octet-stream', 'Cache-Control': 'no-store' })
    res.end(body)
  } catch {
    res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' })
    res.end('404 Not Found')
  }
}).listen(port, '0.0.0.0', () => {
  console.log(`V4.1 原型服务已启动: http://0.0.0.0:${port}/  (root: ${root})`)
})
