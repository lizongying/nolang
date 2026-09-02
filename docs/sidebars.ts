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
      items: [
        "std/overview",
        "std/basic-types",
        "std/core-library",
        "std/os-and-files",
        "std/time-and-date",
        "std/logging",
        "std/data-structures",
        "std/database",
        "std/encoding",
        "std/archives",
        "std/crypto",
        "std/data-exchange",
        "std/async",
        "std/global",
        "std/others",
        "std/module-overview",
      ],
    },
  ],
};

export default sidebars;
