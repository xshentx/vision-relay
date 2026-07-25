package server

import "net/url"

type breakArmorCodexXBundledDescriptor struct {
	FileName    string
	Description string
}

// The allowlisted catalog is compiled in, but the upstream prompt bodies are
// deliberately not embedded in the executable. Some security products scan PE
// resources and classify the security-research terminology in those documents
// as malware. Users can fetch and cache these read-only data files explicitly
// with the "GitHub update" action.
var breakArmorCodexXBundledCatalog = []breakArmorCodexXBundledDescriptor{
	{FileName: "gpt5.5-unrestricted.md", Description: "短小通用，适合日常编码与常规技术任务"},
	{FileName: "gpt5.4-unrestricted.md", Description: "面向 GPT-5.4 / Codex CLI，偏 CTF 与安全研究工作流"},
	{FileName: "gpt5.5-jeli.md", Description: "大白话通用版本，提供更完整的工程与逆向执行流程"},
	{FileName: "gpt-5.6-sol-unrestricted.md", Description: "GPT-5.6-sol 破甲提示词，偏直接执行与中英文任务"},
	{FileName: "海鸥3.0破甲.md", Description: "中文技术操作员人格，覆盖编码、CTF、逆向、内存与协议任务路由"},
}

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

func bundledBreakArmorCodexXTemplates(string) []breakArmorSavedTemplate { return nil }
