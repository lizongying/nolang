import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    "intro",
    {
      type: "category",
      label: "語法",
      items: ["lang/syntax", "lang/str"],
    },
    {
      type: "category",
      label: "運算符",
      items: ["lang/symbol"],
    },
    {
      type: "category",
      label: "標準庫",
      items: ["std/overview"],
    },
  ],
};

export default sidebars;
