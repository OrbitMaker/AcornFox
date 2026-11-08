// 颜色、字号、圆角取自控制台样稿（shadcn zinc 中性色，紧凑字号，13px 正文）。
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
      fontSize: {
        '2xs': ['10.5px', '14px'], xs: ['11.5px', '16px'], sm: ['13px', '19px'], base: ['14px', '21px'],
        lg: ['16px', '22px'], xl: ['18px', '24px'], '2xl': ['22px', '28px'],
      },
      borderRadius: { lg: 'var(--radius)', md: 'calc(var(--radius) - 2px)', sm: 'calc(var(--radius) - 4px)' },
      fontFamily: { sans: ['var(--font-sans)'], mono: ['var(--font-mono)'] },
      keyframes: {
        fadein: { from: { opacity: '0' }, to: { opacity: '1' } },
        popin: { from: { opacity: '0', transform: 'translateY(4px) scale(.98)' }, to: { opacity: '1', transform: 'none' } },
        slidein: { from: { transform: 'translateX(24px)', opacity: '0' }, to: { transform: 'none', opacity: '1' } },
        slideinleft: { from: { transform: 'translateX(-24px)', opacity: '0' }, to: { transform: 'none', opacity: '1' } },
      },
      animation: {
        fade: 'fadein .15s ease-out', in: 'popin .16s ease-out',
        slide: 'slidein .2s ease-out', 'slide-left': 'slideinleft .2s ease-out',
      },
    },
  },
  plugins: [],
} satisfies Config
