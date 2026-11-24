// 颜色、字号、圆角取自控制台样稿（shadcn zinc 中性色；样稿第 14 版起正文 14px）。字号和控件高度的数值在 src/index.css 的“大小档位”。
import type { Config } from 'tailwindcss'

const hsl = (v: string) => `hsl(var(--${v}) / <alpha-value>)`

export default {
  darkMode: ['selector', '[data-theme="dark"]'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        border: hsl('border'), input: hsl('input'), ring: hsl('ring'),
        background: hsl('background'), foreground: hsl('foreground'),
        primary: { DEFAULT: hsl('primary'), foreground: hsl('primary-foreground') },
        secondary: { DEFAULT: hsl('secondary'), foreground: hsl('secondary-foreground') },
        muted: { DEFAULT: hsl('muted'), foreground: hsl('muted-foreground') },
        accent: { DEFAULT: hsl('accent'), foreground: hsl('accent-foreground') },
        destructive: { DEFAULT: hsl('destructive'), foreground: hsl('destructive-foreground') },
        card: { DEFAULT: hsl('card'), foreground: hsl('card-foreground') },
        popover: { DEFAULT: hsl('popover'), foreground: hsl('popover-foreground') },
        sidebar: hsl('sidebar'),
        success: hsl('success'), warning: hsl('warning'), danger: hsl('danger'),
      },
      // 大小档位（样稿第 11 版）：字号、行高、控件高度都指到 index.css 里“大小档位”那一组变量，这里不写死数值。
      // 默认一组就是定下来的大小（正文 14、按钮高 36）；根节点 data-size="sm" 是原来的小一档，只留着对比。页面里不许写死字号和控件高度。
      // 加了新的档名要同时登记到 src/lib/utils.ts（cn 合并类名时要认得它们）。
      fontSize: {
        '2xs': ['var(--fs-2xs)', 'var(--lh-2xs)'], // 角标、状态字（加粗一档）、时间、坐标轴
        xs: ['var(--fs-xs)', 'var(--lh-xs)'], // 次要文字、小号按钮、分段标签、小卡片标题
        sm: ['var(--fs-sm)', 'var(--lh-sm)'], // 正文
        base: ['var(--fs-base)', 'var(--lh-base)'], // 弹窗标题、应用中心的区块标题
        lg: ['var(--fs-lg)', 'var(--lh-lg)'], // 应用详情的应用名、地址
        xl: ['var(--fs-xl)', 'var(--lh-xl)'], // 首页图表里的大数字
        '2xl': ['var(--fs-2xl)', 'var(--lh-2xl)'], // 应用概览里的大数字
        page: ['var(--fs-page)', 'var(--lh-page)'], // 顶栏里的页名
        title: ['var(--fs-title)', 'var(--lh-title)'], // 应用中心里应用页、解决方案页的名字
        section: ['var(--fs-section)', 'var(--lh-section)'], // 区块标题（“需要你处理”“我的应用”）
        // 输入框里的字（手机上 16，免得点进去时页面被放大）。不叫 input：text-input 已经是“输入框边线的颜色”那个类名
        field: ['var(--fs-field)', 'var(--lh-sm)'],
      },
      // 控件高度：h-ctl 默认按钮和输入框、h-ctl-sm 小号、h-ctl-xs 最小、h-ctl-lg 大号；h-nav 左侧菜单每项；h-bar 顶栏；
      // size-icon 按钮和行里的图标；py-row 一行一项的上下留白；h-tabs 分段标签的槽；
      // size-tile-xs / sm / md / lg 应用图块；size-donut 应用概览里的圆环。开关的大小在 index.css 的 .switch（--sw-w、--sw-h）。
      spacing: {
        ctl: 'var(--ctl)', 'ctl-sm': 'var(--ctl-sm)', 'ctl-xs': 'var(--ctl-xs)', 'ctl-lg': 'var(--ctl-lg)',
        nav: 'var(--nav)', bar: 'var(--bar)', icon: 'var(--icon)', row: 'var(--row)', tabs: 'var(--tabs)',
        'tile-xs': 'var(--tile-xs)', 'tile-sm': 'var(--tile-sm)', 'tile-md': 'var(--tile-md)', 'tile-lg': 'var(--tile-lg)', donut: 'var(--donut)',
      },
      // 圆角四档（样稿第 12 版，数值在 index.css）：rounded-sm 和 rounded 4（小件）、rounded-md 8（控件）、rounded-lg 12（卡片、浮层）、rounded-xl 16（弹窗、提示）。
      borderRadius: { xl: 'var(--radius-xl)', lg: 'var(--radius-lg)', md: 'var(--radius-md)', sm: 'var(--radius-sm)', DEFAULT: 'var(--radius-sm)' },
      fontFamily: { sans: ['var(--font-sans)'], mono: ['var(--font-mono)'] },
      // 动画档位（样稿第 12 版，曲线和时长在 index.css）：transition-* 不另写曲线和时长时就是“悬停”那一档（ease-out、--dur-hover）。
      // 进场、离场的关键帧在 index.css 的 .ov-*（配合 lib/hooks.ts 的 usePresence），这里不再有 animate-in、animate-fade、animate-slide。
      transitionTimingFunction: {
        DEFAULT: 'var(--ease-out)', out: 'var(--ease-out)', 'out-strong': 'var(--ease-out-strong)', in: 'var(--ease-in)',
        'in-out': 'var(--ease-in-out)', overshoot: 'var(--ease-overshoot)',
      },
      transitionDuration: { DEFAULT: 'var(--dur-hover)', press: 'var(--dur-press)', move: 'var(--dur-move)', 'md-in': 'var(--dur-md-in)' },
    },
  },
  plugins: [],
} satisfies Config
