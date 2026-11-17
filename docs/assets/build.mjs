// Builds the light and dark variant of every figure from one template.
//
//   node docs/assets/build.mjs
//
// Templates live in docs/assets/src/*.svg and use CSS variables (var(--ink), var(--violet), ...).
// Each output file gets a fixed palette, with no prefers-color-scheme inside the SVG: GitHub renders
// SVGs as images that follow the visitor's OS setting, not their GitHub theme, so the README picks
// the variant with <picture> instead. Colors are the dataviz reference palette's categorical hues,
// light and dark steps (blue = live traffic, aqua = offline loop, violet = the gate, orange = repair).
import { readFileSync, writeFileSync, readdirSync } from "node:fs";
import { join, dirname, basename } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const themes = {
  light: {
    ink: "#1f2328", muted: "#59636e", faint: "#818b98", line: "#d1d9e0", page: "#ffffff",
    card: "#f6f8fa", body: "#ffffff", glass: "#eef3ff",
    blue: "#2a78d6", aqua: "#1baf7a", violet: "#6250d6", orange: "#eb6834", amber: "#eda100",
    red: "#e34948", green: "#008300",
    "blue-soft": "#e8f1fc", "aqua-soft": "#e6f6ef", "violet-soft": "#efedfc", "orange-soft": "#fdeee7",
    "red-soft": "#fdecec", "green-soft": "#e6f3e6", "amber-soft": "#fdf4e0",
    wood: "#c98a4b", metal: "#9aa4ae", glint: "#ffffff",
  },
  dark: {
    ink: "#e6edf3", muted: "#9198a1", faint: "#6e7681", line: "#3d444d", page: "#0d1117",
    card: "#151b23", body: "#0d1117", glass: "#1a2233",
    blue: "#3987e5", aqua: "#199e70", violet: "#9085e9", orange: "#d95926", amber: "#c98500",
    red: "#e66767", green: "#0ca30c",
    "blue-soft": "#132338", "aqua-soft": "#0f2a21", "violet-soft": "#211d3d", "orange-soft": "#33190f",
    "red-soft": "#361818", "green-soft": "#0f2a0f", "amber-soft": "#2e2209",
    wood: "#a8733e", metal: "#adb5bd", glint: "#e6edf3",
  },
};

const srcDir = join(here, "src");
for (const file of readdirSync(srcDir).filter((f) => f.endsWith(".svg"))) {
  const tpl = readFileSync(join(srcDir, file), "utf8");
  if (!tpl.includes("/*THEME*/")) throw new Error(`${file}: missing /*THEME*/ marker in <style>`);
  for (const [mode, vars] of Object.entries(themes)) {
    const css = ":root{" + Object.entries(vars).map(([k, v]) => `--${k}:${v}`).join(";") + "}";
    const out = join(here, `${basename(file, ".svg")}-${mode}.svg`);
    writeFileSync(out, tpl.replace("/*THEME*/", css));
    console.log("wrote", out);
  }
}
