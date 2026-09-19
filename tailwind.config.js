/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./cmd/web/templates/**/*.templ",
    "./static/js/**/*.js"
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        black: 'rgb(var(--rw-canvas-rgb) / <alpha-value>)',
        white: 'rgb(var(--rw-ink-rgb) / <alpha-value>)',
        gray: {
          10: 'rgb(var(--rw-surface-rgb) / <alpha-value>)',
          20: 'rgb(var(--rw-raised-rgb) / <alpha-value>)',
          50: 'rgb(var(--rw-ink-rgb) / <alpha-value>)',
          100: 'rgb(var(--rw-ink-rgb) / <alpha-value>)',
          200: 'rgb(var(--rw-ink-rgb) / <alpha-value>)',
          300: 'rgb(var(--rw-muted-rgb) / <alpha-value>)',
          400: 'rgb(var(--rw-muted-rgb) / <alpha-value>)',
          500: 'rgb(var(--rw-muted-rgb) / <alpha-value>)',
          600: 'rgb(var(--rw-line-rgb) / <alpha-value>)',
          700: 'rgb(var(--rw-line-rgb) / <alpha-value>)',
          800: 'rgb(var(--rw-raised-rgb) / <alpha-value>)',
          900: 'rgb(var(--rw-surface-rgb) / <alpha-value>)',
          950: 'rgb(var(--rw-canvas-rgb) / <alpha-value>)',
        },
        neutral: Object.fromEntries([50,100,200,300,400,500,600,700,800,900,950].map(level => [level,
          `rgb(var(--rw-${level<=200?'ink':level<=500?'muted':level<=700?'line':level===800?'raised':level===900?'surface':'canvas'}-rgb) / <alpha-value>)`])),
        canvas: 'rgb(var(--rw-canvas-rgb) / <alpha-value>)',
        surface: 'rgb(var(--rw-surface-rgb) / <alpha-value>)',
        ink: 'rgb(var(--rw-ink-rgb) / <alpha-value>)',
        muted: 'rgb(var(--rw-muted-rgb) / <alpha-value>)',
        primary: 'rgb(var(--rw-accent-rgb) / <alpha-value>)',
        secondary: 'rgb(var(--rw-accent-rgb) / <alpha-value>)',
      },
      fontFamily: {
        'mono': ['Tomorrow', 'Courier New', 'monospace'],
        'sans': ['Tomorrow', 'system-ui', 'sans-serif'],
        'display': ['Orbitron', 'Tomorrow', 'system-ui', 'sans-serif'],
        'emoji': ['Blobmoji', 'Apple Color Emoji', 'Segoe UI Emoji', 'Segoe UI Symbol'],
      },
      screens: {
        'ultra': '2400px',
      },
      maxWidth: {
        'wide': '120rem',   /* 1920px */
        'ultra': '150rem',  /* 2400px */
      },
      borderRadius: {
        'none': '0',
        DEFAULT: '0', sm: '0', md: '0', lg: '0', xl: '0', '2xl': '0', '3xl': '0',
      },
    }
  },
  plugins: []
}
