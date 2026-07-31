# Mdocman 架构与数据图

## 系统数据流

```mermaid
flowchart LR
    UI[TypeScript 管理界面] -->|REST /api| Admin[mdocman-admin 管理端]
    UI -->|选择/新建| Registry[(~/.mdoc/*.db)]
    Admin --> Registry
    Admin -->|本地句向量 / 混合 RAG| Semantic[(semantic_chunks)]
    Semantic --> Registry
    UI -->|图片上传| Assets[~/.mdoc/uploads]
    Admin -->|内容哈希增量生成| Static[public-site 静态文件]
    Static --> Site[mdocman-site 只读发布端]
    UI -->|目录导入| Import[Markdown 文件]
    Registry -->|ZIP 导出| Export[保留目录层级的 Markdown]
```

## SQLite 实体关系

```mermaid
erDiagram
    NOTEBOOKS ||--o{ FOLDERS : contains
    NOTEBOOKS ||--o{ DOCUMENTS : owns
    FOLDERS ||--o{ FOLDERS : "parent_id 递归"
    FOLDERS ||--o{ DOCUMENTS : contains
    NOTEBOOKS {
        text id PK
        text title
        text description
        text accent
        integer position
    }
    FOLDERS {
        text id PK
        text notebook_id FK
        text parent_id FK
        text title
        integer position
    }
    DOCUMENTS {
        text id PK
        text notebook_id FK
        text folder_id FK
        text title
        text content
        integer position
        text updated_at
    }
```
