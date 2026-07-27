import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    "intro",
    "usage",
    "benchmarks",
    "playground",
    {
      type: "category",
      label: "Language",
      items: ["lang/syntax", "lang/code-style", "lang/str", "lang/symbol", "lang/export", "lang/memory"],
    },
    {
      type: "category",
      label: "Standard Library",
      items: ["std/overview"],
    },
  ],
};

export default sidebars;
