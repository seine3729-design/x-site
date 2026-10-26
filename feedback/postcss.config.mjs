/** Tailwind 4 通过 PostCSS 插件接入，不需要 tailwind.config.js（token 写在 globals.css 的 @theme 里）。 */
const config = {
  plugins: {
    '@tailwindcss/postcss': {},
  },
}

export default config
