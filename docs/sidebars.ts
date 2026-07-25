import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    "intro",
    "usage",
    "benchmarks",
    {
      type: "category",
      label: "Language",
      items: ["lang/syntax", "lang/code-style", "lang/str", "lang/export", "lang/memory"],
    },
    {
      type: "category",
      label: "Operators",
      items: ["lang/symbol"],
    },
    {
      type: "category",
      label: "Standard Library",
      items: ["std/overview"],
    },
  ],
};

export default sidebars;
