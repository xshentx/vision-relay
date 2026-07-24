package server

import (
	"embed"
	"net/url"
	"strings"
)

const breakArmorCodexXBundledRevision = "e8b0e5b73c508484cfb636339c82d70360487442"

type breakArmorCodexXBundledDescriptor struct {
	FileName    string
	Description string
}

var breakArmorCodexXBundledCatalog = []breakArmorCodexXBundledDescriptor{
	{FileName: "gpt5.5-unrestricted.md", Description: "短小通用，适合日常编码与常规技术任务"},
	{FileName: "gpt5.4-unrestricted.md", Description: "面向 GPT-5.4 / Codex CLI，偏 CTF 与安全研究工作流"},
	{FileName: "gpt5.5-jeli.md", Description: "大白话通用版本，提供更完整的工程与逆向执行流程"},
	{FileName: "gpt-5.6-sol-unrestricted.md", Description: "GPT-5.6-sol 破甲提示词，偏直接执行与中英文任务"},
	{FileName: "海鸥3.0破甲.md", Description: "中文技术操作员人格，覆盖编码、CTF、逆向、内存与协议任务路由"},
}

//go:embed break_armor_codex_x_templates/*.md break_armor_codex_x_templates/LICENSE.codex-x
var breakArmorCodexXBundledFS embed.FS

func codexXTemplateDescription(name string) string {
	for _, item := range breakArmorCodexXBundledCatalog {
		if item.FileName == name {
			return item.Description
		}
	}
	return ""
}

func codexXTemplateSourceURL(name string) string {
	return breakArmorCodexXRepoURL + "/blob/main/examples/" + url.PathEscape(name)
}

func bundledBreakArmorCodexXTemplates(client string) []breakArmorSavedTemplate {
	items := make([]breakArmorSavedTemplate, 0, len(breakArmorCodexXBundledCatalog))
	for _, descriptor := range breakArmorCodexXBundledCatalog {
		raw, err := breakArmorCodexXBundledFS.ReadFile("break_armor_codex_x_templates/" + descriptor.FileName)
		if err != nil {
			panic("missing bundled Codex-X template: " + descriptor.FileName)
		}
		items = append(items, breakArmorSavedTemplate{
			ID:             codexXTemplateID(descriptor.FileName),
			Client:         client,
			Name:           descriptor.FileName,
			Description:    descriptor.Description,
			Prompt:         strings.TrimSpace(string(raw)),
			Builtin:        true,
			Bundled:        true,
			ReadOnly:       true,
			Source:         breakArmorCodexXSource,
			SourceURL:      codexXTemplateSourceURL(descriptor.FileName),
			SourceRevision: breakArmorCodexXBundledRevision,
		})
	}
	return items
}
