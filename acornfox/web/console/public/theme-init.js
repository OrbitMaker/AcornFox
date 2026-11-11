// 页面渲染前按本机记住的深浅色设置 data-theme、按记住的主色设置 data-accent，避免先闪一下默认的样子。
// 单独成文件是因为控制台的 CSP 不允许内联脚本。
(function () {
  try {
    var t = localStorage.getItem('afx-theme')
    if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t)
    var a = localStorage.getItem('afx-accent')
    if (a === 'blue' || a === 'green' || a === 'orange') document.documentElement.setAttribute('data-accent', a)
  } catch (e) { /* 本机存储不可用时跟随系统 */ }
})()
