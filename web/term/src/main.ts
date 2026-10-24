import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { closeText, inputMessages, protocols, readParams, resizeMessage, wsURL } from "./protocol.ts";

// 令牌留在地址的片段中：片段不发给服务器，页面嵌在控制台的 iframe 中，用户看不到地址栏。
// 不删除令牌，用户刷新页面后才能重新连接同一个 agent 会话（docs/architecture.md 第 6.3 节）。
const params = readParams(location.href);

const el = document.getElementById("term")!;
// 不加载链接插件和剪贴板插件。终端输出来自沙箱，只显示为文字。
// xterm 自己会识别 OSC 8 超链接，没有 linkHandler 时点击会弹出确认框后打开。这里让点击什么也不做。
const term = new Terminal({ cursorBlink: true, linkHandler: { activate: () => {} } });
const fit = new FitAddon();
term.loadAddon(fit);
term.open(el);
fit.fit();

if (params === null) {
  term.write("缺少工作舱或令牌。请从控制台重新打开运行页面。");
} else {
  const ws = new WebSocket(wsURL(location.href, params.cabin), protocols(params.token));
  ws.binaryType = "arraybuffer";
  const send = (msg: string) => {
    if (ws.readyState === WebSocket.OPEN) {
      ws.send(msg);
    }
  };
  ws.onopen = () => {
    send(resizeMessage(term.cols, term.rows));
    term.focus();
  };
  ws.onmessage = (e) => {
    if (e.data instanceof ArrayBuffer) {
      term.write(new Uint8Array(e.data));
    }
  };
  ws.onclose = (e) => term.write("\r\n\r\n" + closeText(e.code) + "\r\n");
  // 粘贴时 onData 一次给出整段文字，分成多条消息才不超过 webterm 的上限。
  term.onData((data) => inputMessages(data).forEach(send));
  term.onResize(({ cols, rows }) => send(resizeMessage(cols, rows)));
  window.addEventListener("resize", () => fit.fit());
}
