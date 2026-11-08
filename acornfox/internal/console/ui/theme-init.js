// 页面渲染前按本机记住的深浅色设置 data-theme，避免先闪一下浅色。
// 单独成文件是因为控制台的 CSP 不允许内联脚本。
(function () {
  try {
    var t = localStorage.getItem('afx-theme')
    if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t)
  } catch (e) { /* 本机存储不可用时跟随系统 */ }
})()
