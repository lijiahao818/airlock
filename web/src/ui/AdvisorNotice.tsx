// 安全副驾的首次说明。开启前必须给用户看（CLAUDE.md 第 14 节和 product.md 的 F-1）。
export default function AdvisorNotice() {
  return (
    <div className="card notice">
      <h3>开启安全副驾前，请先了解</h3>
      <p>
        开启后，出现拦截时 Airlock 会把少量信息发给模型供应商，换来一段看得懂的风险解释。
        发送的信息只有这些：要访问的主机名、端口、请求方法、路径、命中的规则、agent（就是你在工作舱里用的
        AI 助手）的名字。不发送文件内容，也不发送密钥。
      </p>
      <p>
        模型供应商是 Anthropic 或 DeepSeek，取决于你保存了哪家的密钥；两家都保存时可以自己选。
        你随时可以在密钥管理页关闭安全副驾。关闭后，拦截提示改用固定模板解释，安全判定不受影响。
      </p>
    </div>
  );
}
