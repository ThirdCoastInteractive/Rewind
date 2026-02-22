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
        black: '#000000',
        white: '#FFFFFF',
        gray: {
          10: '#1A1A1A',
          20: '#333333',
        },
        // Legacy colors - to be removed after migration
        primary: '#3b82f6',
        secondary: '#8b5cf6',
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
      },
    }
  },
  plugins: []
}
