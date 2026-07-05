import vue from 'eslint-plugin-vue'
import vueTsConfig from '@vue/eslint-config-typescript'

export default [
  { ignores: ['dist/**', 'node_modules/**'] },
  ...vue.configs['flat/recommended'],
  ...vueTsConfig(),
  {
    rules: {
      'vue/multi-word-component-names': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/max-attributes-per-line': 'off',
    },
  },
]
