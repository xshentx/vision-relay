import { copyFile, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const files = [
  ["node_modules/vue/dist/vue.global.prod.js", "public/assets/vendor/vue/vue.global.prod.js"],
  ["node_modules/element-plus/dist/index.full.min.js", "public/assets/vendor/element-plus/index.full.min.js"],
  ["node_modules/element-plus/dist/index.css", "public/assets/vendor/element-plus/index.css"]
];

for (const [source, target] of files) {
  const sourcePath = resolve(frontendRoot, source);
  const targetPath = resolve(frontendRoot, target);
  await mkdir(dirname(targetPath), {recursive: true});
  await copyFile(sourcePath, targetPath);
  console.log(`Synced ${target}`);
}
