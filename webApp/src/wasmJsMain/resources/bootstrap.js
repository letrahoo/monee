/* Native startup feedback must work even when the Compose renderer cannot start. */
(() => {
  'use strict';
  const panel = document.getElementById('startup');
  const title = document.getElementById('startup-title');
  const message = document.getElementById('startup-message');
  const retry = document.getElementById('startup-retry');
  let phase = 'starting';
  let slowTimer;

  const showFailure = (heading, detail) => {
    if (phase !== 'starting') return;
    phase = 'failed';
    clearTimeout(slowTimer);
    title.textContent = heading;
    message.textContent = detail;
    panel.hidden = false;
    panel.setAttribute('role', 'alert');
    panel.setAttribute('aria-busy', 'false');
    retry.hidden = false;
    retry.focus();
  };
  retry.addEventListener('click', () => window.location.reload());
  const onStartupError = () => showFailure('界面启动失败', '界面未能完成加载。请重新加载页面；若仍失败，请检查浏览器兼容性或网络连接。');
  window.addEventListener('error', onStartupError);
  window.addEventListener('unhandledrejection', onStartupError);
  window.moneeStartupReady = () => {
    if (phase !== 'starting') return;
    phase = 'ready';
    clearTimeout(slowTimer);
    panel.hidden = true;
    panel.setAttribute('aria-busy', 'false');
    document.getElementById('monee').removeAttribute('inert');
    window.removeEventListener('error', onStartupError);
    window.removeEventListener('unhandledrejection', onStartupError);
  };

  const supportsWebGL2 = () => {
    let canvas;
    let context;
    try {
      canvas = document.createElement('canvas');
      context = canvas.getContext('webgl2', {
        alpha: true, depth: true, stencil: true, antialias: false,
        premultipliedAlpha: true, preserveDrawingBuffer: false,
        failIfMajorPerformanceCaveat: false
      });
      return !!context && typeof context.isContextLost === 'function' && !context.isContextLost();
    } catch (_) {
      return false;
    } finally {
      if (context) {
        try { context.getExtension('WEBGL_lose_context')?.loseContext(); } catch (_) { /* Drop references even if release is unavailable. */ }
      }
      context = null;
      canvas = null;
    }
  };

  if (typeof WebAssembly !== 'object') {
    showFailure('浏览器不兼容', '请使用支持 WebAssembly GC 和 WebGL 2 的新版浏览器，然后重新加载页面。');
    return;
  }
  if (!supportsWebGL2()) {
    showFailure('无法启动图形界面', '当前浏览器未能创建 WebGL 2 图形上下文。请检查浏览器图形加速设置，或换用支持 WebGL 2 的新版浏览器，然后重新加载页面。');
    return;
  }
  slowTimer = setTimeout(() => {
    if (phase !== 'starting') return;
    message.textContent = '启动时间较长，请检查网络连接，或重新加载页面。';
    panel.setAttribute('aria-busy', 'false');
    retry.hidden = false;
  }, 20000);
  const script = document.createElement('script');
  script.src = 'monee.js';
  script.onerror = onStartupError;
  document.body.appendChild(script);
})();
