# ADR-006 agent 界面嵌入控制台，并与控制台隔离

## 状态

已接受。

## 背景

用户要一边和 agent 对话，一边看 agent 的出网情况。agent 的界面和拦截卡片分在两个窗口时，用户看不出拦截和 agent 当前操作之间的关系。

控制台可以放行请求。agent 的输出来自沙箱，按不可信数据处理。如果 agent 的输出和放行按钮在同一个源中运行，被注入的 agent 可以输出内容来操纵控制台。

## 决策

### 运行页面

控制台的运行页面分为两部分：

| 区域 | 内容 | 来源 |
|---|---|---|
| 左侧 | agent 的界面 | 独立源的 iframe |
| 右侧 | 出网动态和拦截卡片 | 控制台自己的源 |

### 源隔离

- agent 的界面放在 iframe 中。iframe 的端口与控制台不同，所以浏览器把它们当作不同的源。
- 放行按钮只在控制台的源中。iframe 中的内容不能读取或操作控制台页面。
- 控制台接口不接受跨源请求，并要求令牌，见 [security.md](../security.md#5-威胁与防护措施) 的 T-12。

### 终端型 agent

```
iframe（网页终端，独立端口）──WebSocket──► airlock.exe ──docker exec -it──► 沙箱内的 agent
```

- Airlock 在独立的端口上提供网页终端页面。页面用 `@xterm/xterm` 显示终端。
- 网页终端通过 WebSocket 连接 Airlock。Airlock 用 `docker exec -it` 连接沙箱内的 agent。
- WebSocket 握手时，Airlock 检查 Origin 和本次运行的令牌。
- 网页终端不加载链接插件和剪贴板插件。agent 的输出只显示为文字。

### 网页型 agent

- iframe 的地址是入站中继的地址加上令牌，见 [ADR-005](005-inbound-web-ui.md)。

### 保底方式

- 用户也可以在独立的终端窗口中使用终端型 agent。Airlock 在终端窗口中执行 `docker exec -it`。

## 备选方案

| 方案 | 不选的原因 |
|---|---|
| agent 界面和控制台分在不同窗口 | 用户要在窗口之间切换，看不出拦截和 agent 操作的关系。 |
| 在控制台的源中直接显示 agent 输出 | agent 的输出可以操纵有放行权限的页面。 |
| 自己实现终端显示 | 终端控制序列很多，工作量大，容易出错。 |

## 后果

- 好处：用户在一个页面中完成对话和决定。agent 的输出碰不到放行按钮。
- 坏处：新增前端依赖 `@xterm/xterm`。这个包使用 MIT 许可证，来自官方仓库 `xtermjs/xterm.js`，已用 `npm view` 核实。引入时锁定版本。
- Airlock 要维护 WebSocket 到 `docker exec` 的转接。
- 网页终端的端口分配和令牌管理由 `architecture.md` 设计。
