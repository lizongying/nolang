import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Nolang',
  tagline: 'A modern systems programming language',
  favicon: 'img/logo.svg',

  url: 'https://lizongying.github.io',
  baseUrl: '/nolang/',

  organizationName: 'lizongying',
  projectName: 'nolang',

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'zh-Hans',
    locales: ['zh-Hans', 'en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/lizongying/nolang/tree/main/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/docusaurus-social-card.jpg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Nolang',
      // logo: {
      //   alt: 'Nolang Logo',
      //   src: 'img/logo.svg',
      // },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'tutorialSidebar',
          position: 'left',
          label: '文档',
        },
        {
          type: 'localeDropdown',
          position: 'right',
        },
        {
          href: 'https://github.com/lizongying/nolang',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: '文档',
          items: [
            {
              label: '入门',
              to: '/docs/intro',
            },
            {
              label: '语言参考',
              to: '/docs/lang/syntax',
            },
            {
              label: '标准库',
              to: '/docs/std/overview',
            },
          ],
        },
        {
          title: '更多',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/lizongying/nolang',
            },
            {
              label: '报告问题',
              href: 'https://github.com/lizongying/nolang/issues',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Nolang. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['go', 'llvm', 'rust'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
