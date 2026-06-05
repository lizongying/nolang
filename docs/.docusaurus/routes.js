import React from 'react';
import ComponentCreator from '@docusaurus/ComponentCreator';

export default [
  {
    path: '/nolang/__docusaurus/debug',
    component: ComponentCreator('/nolang/__docusaurus/debug', '51a'),
    exact: true
  },
  {
    path: '/nolang/__docusaurus/debug/config',
    component: ComponentCreator('/nolang/__docusaurus/debug/config', '72e'),
    exact: true
  },
  {
    path: '/nolang/__docusaurus/debug/content',
    component: ComponentCreator('/nolang/__docusaurus/debug/content', '23b'),
    exact: true
  },
  {
    path: '/nolang/__docusaurus/debug/globalData',
    component: ComponentCreator('/nolang/__docusaurus/debug/globalData', '365'),
    exact: true
  },
  {
    path: '/nolang/__docusaurus/debug/metadata',
    component: ComponentCreator('/nolang/__docusaurus/debug/metadata', 'bf5'),
    exact: true
  },
  {
    path: '/nolang/__docusaurus/debug/registry',
    component: ComponentCreator('/nolang/__docusaurus/debug/registry', 'b65'),
    exact: true
  },
  {
    path: '/nolang/__docusaurus/debug/routes',
    component: ComponentCreator('/nolang/__docusaurus/debug/routes', '10a'),
    exact: true
  },
  {
    path: '/nolang/markdown-page',
    component: ComponentCreator('/nolang/markdown-page', '4a8'),
    exact: true
  },
  {
    path: '/nolang/docs',
    component: ComponentCreator('/nolang/docs', '9c7'),
    routes: [
      {
        path: '/nolang/docs',
        component: ComponentCreator('/nolang/docs', 'e10'),
        routes: [
          {
            path: '/nolang/docs',
            component: ComponentCreator('/nolang/docs', 'bd1'),
            routes: [
              {
                path: '/nolang/docs/intro',
                component: ComponentCreator('/nolang/docs/intro', '81f'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/docs/lang/syntax',
                component: ComponentCreator('/nolang/docs/lang/syntax', '909'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/docs/std/overview',
                component: ComponentCreator('/nolang/docs/std/overview', 'cfa'),
                exact: true,
                sidebar: "tutorialSidebar"
              }
            ]
          }
        ]
      }
    ]
  },
  {
    path: '/nolang/',
    component: ComponentCreator('/nolang/', '052'),
    exact: true
  },
  {
    path: '*',
    component: ComponentCreator('*'),
  },
];
