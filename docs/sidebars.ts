import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: '语言参考',
      items: ['lang/syntax'],
    },
    {
      type: 'category',
      label: '标准库',
      items: ['std/overview'],
    },
  ],
};

export default sidebars;
