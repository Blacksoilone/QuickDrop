package server

// indexHTMLTpl 主页模板, 占位符顺序:
//
//	%s = 文件名 (已 HTML 转义)
//	%s = 文件大小描述 (已 HTML 转义)
const indexHTMLTpl = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>QuickDrop</title>
  <style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
      max-width: 520px; margin: 24px auto; padding: 16px;
      color: #1a1a1a; background: #fafafa;
    }
    h1 { font-size: 1.3em; margin: 0 0 16px; }
    section {
      padding: 18px; margin-bottom: 14px;
      background: #fff; border: 1px solid #e5e5e5; border-radius: 10px;
    }
    section h2 { font-size: 1em; margin: 0 0 12px; color: #555; font-weight: 600; }
    .qr-card { text-align: center; }
    .qr-card img { width: 220px; height: 220px; display: block; margin: 0 auto 8px; }
    .qr-card p { color: #888; font-size: .9em; margin: 0; }
    .file-card .name { font-weight: 600; word-break: break-all; margin-bottom: 4px; }
    .btn {
      display: inline-block; padding: 10px 18px; font-size: 1em;
      background: #0066ff; color: #fff; border: 0; border-radius: 6px;
      text-decoration: none; cursor: pointer; -webkit-appearance: none;
    }
    input[type=file] {
      width: 100%%; margin-bottom: 12px; padding: 8px;
      background: #fafafa; border: 1px dashed #ccc; border-radius: 6px;
    }
    .hint { color: #888; font-size: .85em; margin: 8px 0 12px; }
    @media (prefers-color-scheme: dark) {
      body { color: #eee; background: #1a1a1a; }
      section { background: #262626; border-color: #333; }
      section h2 { color: #aaa; }
      .qr-card img { background: #fff; padding: 6px; border-radius: 4px; }
      input[type=file] { background: #1a1a1a; border-color: #444; color: #eee; }
    }
  </style>
</head>
<body>
  <h1>QuickDrop</h1>

  <section class="qr-card">
    <h2>手机扫码</h2>
    <img src="/qr" alt="扫码进入此页面">
    <p>同 WiFi 的手机用系统相机扫上方二维码</p>
  </section>

  <section class="file-card">
    <h2>下载到此设备</h2>
    <p class="name">%s</p>
    <p class="hint">%s</p>
    <a class="btn" href="/file">下载</a>
  </section>

  <section>
    <h2>上传到电脑</h2>
    <form action="/upload" method="post" enctype="multipart/form-data">
      <input type="file" name="file" multiple required>
      <button class="btn" type="submit">上传</button>
    </form>
    <p class="hint">保存到电脑 ~/Downloads/QuickDrop/</p>
  </section>
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
    a { display: inline-block; padding: 10px 18px; background: #0066ff; color: #fff; border-radius: 6px; text-decoration: none; margin-top: 16px; }
  </style>
</head>
<body>
  <h1>已收到 %d 个文件</h1>
  <p>已保存到电脑 ~/Downloads/QuickDrop/</p>
  <a href="/">返回</a>
</body>
</html>`
