# 预设样式模板说明

此目录存放 `write_docx(style_preset=...)` 使用的 .docx 样式模板文件。

当前 preset 使用 wordZero 内置 API 生成结构化元素（封面、目录、页眉页脚等），
无需额外的模板文件。`template_path` 参数允许用户指定自定义 .docx 模板文件
来继承其样式定义（字体、段落样式、表格样式等）。

## 使用方式

```toml
# Rexion Skill 中调用:
write_docx(
  path="周报.docx",
  content="# 周报\n...",
  style_preset="report",       # 自动封面+目录+页眉页脚
  table_style="professional",  # 表格蓝色标题行+thin borders
)

# 自定义模板:
write_docx(
  path="合同.docx",
  content="...",
  template_path=".rexion/templates/styles/company-template.docx",  # 继承公司模板样式
)
```

## 预设对照表

| preset | 封面 | 目录 | 页眉 | 页脚 | 签名/落款 |
|--------|------|------|------|------|-----------|
| plain  | ❌ | ❌ | ❌ | ❌ | ❌ |
| report | ✅ | ✅ | ✅ (标题) | ✅ (页码) | ❌ |
| contract | ✅ (居中) | ❌ | ✅ ("合同文件") | ✅ (页码) | ✅ (甲方/乙方签章) |
| minutes | ✅ (居中) | ❌ | ✅ ("会议纪要") | ✅ (页码) | ❌ |
| letter | ❌ | ❌ | ❌ | ❌ | ✅ (此致敬礼) |

## 自定义模板

将你公司的 .docx 模板文件放入此目录，然后通过 `template_path` 引用。
模板文件应包含所需的样式定义（heading styles、paragraph styles、table styles），
wordZero 会读取这些样式并应用到新文档的内容上。
