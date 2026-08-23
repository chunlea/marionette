export default {
  plugins: {
    // Tailwind 4 ships its own PostCSS plugin and handles vendor prefixing
    // itself, so autoprefixer is gone. Theme configuration lives in
    // src/index.css under @theme — there is no tailwind.config.js any more.
    '@tailwindcss/postcss': {},
  },
}
