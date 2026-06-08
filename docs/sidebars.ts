import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: '語法',
      items: ['lang/syntax'],
    },
    {
      type: 'category',
      label: '關鍵字',
      items: ['lang/keywords'],
    },
    {
      type: 'category',
      label: '標準庫',
      items: ['std/overview'],
    },
  ],
};

export default sidebars;
