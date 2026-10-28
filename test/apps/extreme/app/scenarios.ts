// The scenarios of the app, each with the sizes the tests run. A size is a path parameter, so
// any other size within the limits of the scenario's Load works too.
export const scenarios = [
  { path: '/dom', title: 'DOM 数量', note: '大 PR 的文件变更页，按元素数', sizes: [10000, 50000, 100000] },
  { path: '/nest', title: '组件嵌套深度', note: '只传 children 的组件，DOM 不超过 400 层', sizes: [500, 1000, 5000, 10000] },
  { path: '/deep', title: 'DOM 嵌套深度', note: '层层嵌套的 div', sizes: [400, 600] },
  { path: '/inject', title: '注入大数据', note: '商品目录 JSON，按 MB', sizes: [1, 5, 20] },
  { path: '/json', title: '数据嵌套', note: 'JSON 嵌套层数', sizes: [1999, 2001] },
  { path: '/table', title: '长表格', note: 'TanStack Table，10 列', sizes: [1000, 5000, 10000] },
  { path: '/providers', title: 'Provider 层数', note: '每层一个不同的 Context', sizes: [200, 500, 1000] },
  { path: '/charts', title: 'SVG 图表', note: '服务端画的折线、柱状、面积、散点图', sizes: [12] },
  { path: '/markdown', title: '长 Markdown', note: '带高亮代码的长文，按 KB', sizes: [200, 1000, 2000] },
  { path: '/i18n', title: 'i18n 和 Intl', note: '文案键数，另有 5000 次 Intl 调用', sizes: [3000] },
  { path: '/mall', title: '大站商品页', note: '按亚马逊和京东商品页的量', sizes: ['zh-CN', 'en-US'] },
  { path: '/console', title: '后台忙碌的一天', note: '按 react-admin 和 GitHub 长 issue 的量', sizes: [100] },
] as const
