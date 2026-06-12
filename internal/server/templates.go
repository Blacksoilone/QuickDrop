package server

// 三套页面 (ADR-17 极简 UI):
//
//   dashboardHTMLTpl  电脑端 webview 弹窗加载的 /。极简: QR + 文件名 + 大小 + 关闭。
//   downloadHTMLTpl   手机端扫码进的 /d。文件图标 + 文件名 + 大小 + 下载按钮, 无 QR。
//   uploadHTMLTpl     手机端接收模式 /u。上传表单 (Phase 2.13 才挂载, 默认 404)。
//   uploadDoneHTMLTpl 上传完成回执 (Phase 2.13 复用)。
//
// 占位符注意: 模板里所有字面 % 必须写成 %% (因为是 fmt.Fprintf 的 format string).

// dashboardHTMLTpl 占位:
//
//	%s = 文件名 (已 HTML 转义)
//	%s = 文件大小描述 (已 HTML 转义)
const dashboardHTMLTpl = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QuickDrop</title>
  <style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    html, body {
      width: 100%%; height: 100%%;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
      color: #1a1a1a; background: #fafafa;
      overflow: hidden;
    }
    .wrap {
      width: 100%%; height: 100%%;
      display: flex; flex-direction: column; align-items: center;
      padding: 12px 12px 14px;
    }
    .qr {
      width: 240px; height: 240px;
      background: #fff; padding: 6px; border-radius: 6px;
      flex: 0 0 auto;
    }
    .qr img { width: 100%%; height: 100%%; display: block; }
    .name {
      margin-top: 10px; font-size: 13px; font-weight: 600;
      max-width: 100%%; text-align: center;
      white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    }
    .size {
      margin-top: 2px; font-size: 11px; color: #888;
    }
    .close {
      position: absolute; top: 4px; right: 6px;
      width: 22px; height: 22px; line-height: 20px; text-align: center;
      border: 0; background: transparent; cursor: pointer;
      font-size: 16px; color: #888; border-radius: 4px;
    }
    .close:hover { background: #e5e5e5; color: #333; }
    @media (prefers-color-scheme: dark) {
      html, body { color: #eee; background: #1a1a1a; }
      .close:hover { background: #333; color: #fff; }
    }
  </style>
</head>
<body>
  <button class="close" onclick="quickdropClose()" title="关闭 (daemon 继续运行)">×</button>
  <div class="wrap">
    <div class="qr"><img src="/qr" alt="扫码下载"></div>
    <div class="name">%s</div>
    <div class="size">%s</div>
  </div>
</body>
</html>`

// downloadHTMLTpl 手机端发送目标页, 占位:
//
//	%s = 文件名 (已 HTML 转义)
//	%s = 文件大小描述 (已 HTML 转义)
//
// 用 SVG 占位图标. 后续可按 MIME 换缩略图 (图片用 /file 直链, 其他用类型图标).
const downloadHTMLTpl = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QuickDrop - 下载</title>
  <style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
      max-width: 420px; margin: 0 auto; padding: 32px 20px;
      color: #1a1a1a; background: #fafafa;
      min-height: 100vh; display: flex; flex-direction: column;
    }
    .card {
      background: #fff; border: 1px solid #e5e5e5; border-radius: 12px;
      padding: 28px 20px; text-align: center;
    }
    .icon {
      width: 96px; height: 96px; margin: 0 auto 18px;
      display: flex; align-items: center; justify-content: center;
      background: #eef3ff; border-radius: 16px;
    }
    .icon svg { width: 56px; height: 56px; stroke: #0066ff; fill: none; stroke-width: 2; }
    .name {
      font-size: 1.05em; font-weight: 600;
      word-break: break-all; margin-bottom: 6px;
    }
    .size { color: #888; font-size: .9em; margin-bottom: 20px; }
    .btn {
      display: block; width: 100%%; padding: 14px;
      background: #0066ff; color: #fff; border: 0; border-radius: 8px;
      font-size: 1em; font-weight: 600;
      text-decoration: none; cursor: pointer; -webkit-appearance: none;
    }
    @media (prefers-color-scheme: dark) {
      body { color: #eee; background: #1a1a1a; }
      .card { background: #262626; border-color: #333; }
      .icon { background: #1a2640; }
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">
      <svg viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
        <polyline points="14 2 14 8 20 8"/>
      </svg>
    </div>
    <div class="name">%s</div>
    <div class="size">%s</div>
    <a class="btn" href="/file">下载到本机</a>
  </div>
</body>
</html>`

// uploadHTMLTpl 手机端接收模式上传页 (Phase 2.13).
const uploadHTMLTpl = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QuickDrop - 上传</title>
  <style>
    :root { color-scheme: light dark; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
      max-width: 420px; margin: 0 auto; padding: 32px 20px;
      color: #1a1a1a; background: #fafafa;
    }
    .card {
      background: #fff; border: 1px solid #e5e5e5; border-radius: 12px; padding: 24px 20px;
    }
    h1 { font-size: 1.1em; margin: 0 0 16px; }
    input[type=file] {
      width: 100%%; margin-bottom: 16px; padding: 10px;
      background: #fafafa; border: 1px dashed #ccc; border-radius: 8px;
    }
    .btn {
      display: block; width: 100%%; padding: 14px;
      background: #0066ff; color: #fff; border: 0; border-radius: 8px;
      font-size: 1em; font-weight: 600;
    }
    .hint { color: #888; font-size: .85em; margin-top: 12px; text-align: center; }
    @media (prefers-color-scheme: dark) {
      body { color: #eee; background: #1a1a1a; }
      .card { background: #262626; border-color: #333; }
      input[type=file] { background: #1a1a1a; border-color: #444; color: #eee; }
    }
  </style>
</head>
<body>
  <div class="card">
    <h1>上传到电脑</h1>
    <form action="/upload" method="post" enctype="multipart/form-data">
      <input type="file" name="file" multiple required>
      <button class="btn" type="submit">上传</button>
    </form>
    <p class="hint">保存到 ~/Downloads/QuickDrop/</p>
  </div>
</body>
</html>`

const uploadDoneHTMLTpl = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QuickDrop - 上传完成</title>
  <style>
    body { font-family: -apple-system, system-ui, sans-serif; max-width: 480px; margin: 40px auto; padding: 16px; text-align: center; color: #1a1a1a; }
    h1 { font-size: 1.4em; }
  </style>
</head>
<body>
  <h1>已收到 %d 个文件</h1>
  <p>已保存到电脑 ~/Downloads/QuickDrop/</p>
</body>
</html>`
