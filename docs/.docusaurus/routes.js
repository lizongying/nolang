import React from 'react';
import ComponentCreator from '@docusaurus/ComponentCreator';

export default [
  {
    path: '/nolang/en/markdown-page',
    component: ComponentCreator('/nolang/en/markdown-page', '23c'),
    exact: true
  },
  {
    path: '/nolang/en/docs',
    component: ComponentCreator('/nolang/en/docs', '4c1'),
    routes: [
      {
        path: '/nolang/en/docs',
        component: ComponentCreator('/nolang/en/docs', '41b'),
        routes: [
          {
            path: '/nolang/en/docs',
            component: ComponentCreator('/nolang/en/docs', 'e25'),
            routes: [
              {
                path: '/nolang/en/docs/intro',
                component: ComponentCreator('/nolang/en/docs/intro', '638'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/en/docs/lang/export',
                component: ComponentCreator('/nolang/en/docs/lang/export', 'e2f'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/en/docs/lang/str',
                component: ComponentCreator('/nolang/en/docs/lang/str', 'eb1'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/en/docs/lang/symbol',
                component: ComponentCreator('/nolang/en/docs/lang/symbol', 'b27'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/en/docs/lang/syntax',
                component: ComponentCreator('/nolang/en/docs/lang/syntax', 'c5b'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/en/docs/std/overview',
                component: ComponentCreator('/nolang/en/docs/std/overview', '8c2'),
                exact: true,
                sidebar: "tutorialSidebar"
              },
              {
                path: '/nolang/en/docs/usage',
                component: ComponentCreator('/nolang/en/docs/usage', '86f'),
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
    path: '/nolang/en/',
    component: ComponentCreator('/nolang/en/', '4cc'),
    exact: true
  },
  {
    path: '*',
    component: ComponentCreator('*'),
  },
];
