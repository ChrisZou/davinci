package server

import "fmt"

// noFrontendPage is what a user sees when the server binary was built without a
// frontend bundle — it tells them exactly how to fix it instead of showing an
// empty page.
func noFrontendPage(port int) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>davinci</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.6 -apple-system, "PingFang SC", "Helvetica Neue", sans-serif;
         max-width: 40rem; margin: 12vh auto; padding: 0 1.5rem; }
  code { background: rgba(127,127,127,.16); padding: .15em .4em; border-radius: 4px; }
  pre { background: rgba(127,127,127,.12); padding: .8rem 1rem; border-radius: 8px;
        overflow-x: auto; }
  h1 { font-size: 1.5rem; margin-bottom: .2rem; }
  p { opacity: .8; }
</style>
</head>
<body>
<h1>davinci 服务已启动</h1>
<p>API 在 <code>http://127.0.0.1:%d/api</code>，但这个二进制里没有打包前端。</p>
<p>用下面任一方式打开编辑器：</p>
<pre># 开发模式（推荐，改前端即时热更新）
cd web && pnpm dev        # 然后访问 Vite 提示的地址

# 或者先构建再重启服务
cd web && pnpm build && davinci serve</pre>
</body>
</html>`, port)
}
