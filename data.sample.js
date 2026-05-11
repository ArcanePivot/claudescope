// ClaudeScope v1.0 sample dataset.
// 当 generator 还没跑、用户首次打开 index.html 时由此数据兜底渲染骨架。
// schema 与真实 data.js 完全一致：schemaVersion=3、catalog/recordBase/recordsV3、
// 内置 Anthropic 定价、内置 risk 阈值、subagent 合并已应用。
window.CLAUDESCOPE_SAMPLE_DATA = {
  schemaVersion: 3,
  tool: "claudescope",
  generatedAt: "2026-05-09 18:30:00",
  catalog: {
    sessions: [
      ["sample-main-01", "ClaudeScope", "claude-opus-4-7", 0, 6],
      ["sample-main-02", "Design Review", "claude-sonnet-4-6", 1, 4],
      ["sample-main-03", "Quick Help", "claude-haiku-4-5-20251001", 2, 3],
      ["sample-sub-001", "ClaudeScope · subagent c0de1234", "claude-sonnet-4-6", 0, 2]
    ],
    models: [
      "claude-opus-4-7",
      "claude-sonnet-4-6",
      "claude-haiku-4-5-20251001"
    ]
  },
  recordBase: 1778421600000, // 2026-05-09 14:00:00 UTC
  availableRange: {
    start: 1778421600000,
    end: 1778442600000  // 2026-05-09 19:50:00 UTC
  },
  // recordsV3: [tsDelta, sidIdx, modelIdx, input, cacheRead, cacheCreate, output, total, sourceSidIdx, flags]
  // flags 位：0x01 IS_SUBAGENT / 0x02 WAS_SYNTHETIC / 0x04 IS_FAILURE / 0x08 HAS_ERROR / 0x10 IS_THIRD_PARTY
  recordsV3: [
    [      0, 0, 0,  18000,  92000,  6500,   1900,  118400, 0, 0],
    [ 360000, 0, 0,  16500,  88000,  5800,   1700,  112000, 0, 0],
    [ 720000, 1, 1,  21000, 110000,  4200,   2400,  137600, 1, 0],
    [1080000, 2, 2,   8200,  31000,  1500,    900,   41600, 2, 0],
    [1440000, 0, 0,  19500,  95000,  6100,   2050,  122650, 0, 0],
    [1800000, 0, 1,   9800,  46000,  2800,   1100,   59700, 3, 1], // subagent → 计入主会话 0
    [2160000, 1, 1,  17500,  98000,  3700,   2150,  121350, 1, 0],
    [2520000, 2, 2,   6400,  22000,  1100,    700,   30200, 2, 0],
    [2880000, 0, 0,  22000, 104000,  6900,   2300,  135200, 0, 0],
    [3240000, 1, 1,  15000,  82000,  3300,   1800,  102100, 1, 0],
    [3600000, 0, 1,   8200,  39000,  2400,    950,   50550, 3, 1], // subagent
    [3960000, 2, 2,   5800,  19000,   900,    600,   26300, 2, 0],
    [4320000, 0, 0,  20500, 101000,  6400,   2150,  130050, 0, 0],
    [4680000, 1, 1,  16800,  91000,  3500,   2000,  113300, 1, 0],
    [5040000, 0, 0,  17800,  93000,  6200,   1850,  118850, 0, 0]
  ],
  failureRecordsV3: [
    [3600000, 1, 1, 0, 0, 0, 0, 0, 1, 4] // flags 0x04 IS_FAILURE
  ],
  dedupStats: {
    rawUsageRows: 18,
    keptUsageRows: 15,
    duplicatesSkipped: 3,
    uuidFallbackRows: 0,
    weakKeyRows: 0
  },
  filteredCounts: {
    synthetic: 4,
    malformed: 0,
    subagentRows: 2,
    thirdParty: 0
  },
  errorStats: {
    apiErrorsTotal: 1,
    apiErrorsWithUsage: 0,
    apiErrorsNoUsage: 1
  },
  pricingRules: [
    {
      label: "Claude Opus 4.7",
      patterns: ["claude-opus-4-7", "claude-opus-4.7"],
      input: 15.0,
      cacheRead: 1.5,
      cacheCreate: 18.75,
      output: 75.0
    },
    {
      label: "Claude Opus 4.6",
      patterns: ["claude-opus-4-6", "claude-opus-4.6"],
      input: 15.0,
      cacheRead: 1.5,
      cacheCreate: 18.75,
      output: 75.0
    },
    {
      label: "Claude Sonnet 4.6",
      patterns: ["claude-sonnet-4-6", "claude-sonnet-4.6"],
      input: 3.0,
      cacheRead: 0.3,
      cacheCreate: 3.75,
      output: 15.0
    },
    {
      label: "Claude Haiku 4.5",
      patterns: ["claude-haiku-4-5", "claude-haiku-4.5"],
      input: 1.0,
      cacheRead: 0.1,
      cacheCreate: 1.25,
      output: 5.0
    }
  ],
  pricingFallback: {
    label: "未知模型",
    patterns: [],
    input: 0,
    cacheRead: 0,
    cacheCreate: 0,
    output: 0
  },
  pricingSource: "builtin",
  riskConfig: {
    primary: {
      label: "5 小时窗口",
      windowMinutes: 300,
      tokenThreshold: 19000000
    },
    secondary: {
      label: "7 天窗口",
      windowMinutes: 10080,
      tokenThreshold: 200000000
    },
    disclaimer: "本地压力估算 · 不代表 Anthropic 官方剩余额度",
    source: "builtin"
  },
  pressure: {
    primary: {
      label: "5 小时窗口",
      windowMinutes: 300,
      tokenThreshold: 19000000,
      current: {
        tokens: 1419850,
        percent: 7,
        asOf: 1778442600000
      },
      peak: {
        tokens: 1419850,
        percent: 7,
        atTime: 1778442600000
      }
    },
    secondary: {
      label: "7 天窗口",
      windowMinutes: 10080,
      tokenThreshold: 200000000,
      current: {
        tokens: 1419850,
        percent: 1,
        asOf: 1778442600000
      },
      peak: {
        tokens: 1419850,
        percent: 1,
        atTime: 1778442600000
      }
    },
    disclaimer: "本地压力估算 · 不代表 Anthropic 官方剩余额度"
  },
  notes: {
    sample: true,
    thirdPartyDefaultExcluded: true,
    subagentMergeApplied: true,
    pricingApplied: true,
    riskApplied: true,
    disclaimer: "ClaudeScope 内置示例数据；真实指标请运行 generator 生成 data.js"
  }
};
