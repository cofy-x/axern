import sitemap from '@astrojs/sitemap';
import starlight from '@astrojs/starlight';
import { defineConfig } from 'astro/config';
import mermaid from 'astro-mermaid';

const sidebar = [
  {
    label: 'Start',
    translations: { 'zh-cn': '开始' },
    items: [
      { label: 'Overview', translations: { 'zh-cn': '产品概览' }, link: '/' },
      { label: 'Getting Started', translations: { 'zh-cn': '入门' }, link: '/getting-started/' },
      { label: 'Compose Quickstart', translations: { 'zh-cn': 'Compose 快速开始' }, link: '/getting-started/compose/' },
      { label: 'Kubernetes Install', translations: { 'zh-cn': 'Kubernetes 安装' }, link: '/getting-started/kubernetes/' },
    ],
  },
  {
    label: 'Build',
    translations: { 'zh-cn': '构建' },
    items: [
      { label: 'Axern CLI', translations: { 'zh-cn': 'Axern CLI' }, link: '/guides/cli/' },
      { label: 'Identity and Access', translations: { 'zh-cn': '身份与权限' }, link: '/guides/authorization/' },
      { label: 'Catalog and Environments', translations: { 'zh-cn': 'Catalog 与环境' }, link: '/guides/catalog/' },
      { label: 'Secrets', translations: { 'zh-cn': 'Secret' }, link: '/guides/secrets/' },
      { label: 'Python Service', translations: { 'zh-cn': 'Python Service' }, link: '/guides/python-service/' },
      { label: 'Functions', translations: { 'zh-cn': 'Function' }, link: '/guides/functions/' },
      { label: 'Coding Agents', translations: { 'zh-cn': '编码 Agent' }, link: '/guides/agent/' },
      { label: 'SSH Access', translations: { 'zh-cn': 'SSH 访问' }, link: '/guides/ssh/' },
      { label: 'Reverse Tunnels', translations: { 'zh-cn': '反向隧道' }, link: '/guides/tunnels/' },
      { label: 'Storage and Volumes', translations: { 'zh-cn': '存储与卷' }, link: '/guides/storage/' },
    ],
  },
  {
    label: 'SDKs',
    translations: { 'zh-cn': 'SDK' },
    items: [
      { label: 'Overview', translations: { 'zh-cn': '概览' }, link: '/sdk/' },
      { label: 'Python', link: '/sdk/python/' },
      { label: 'Go', link: '/sdk/go/' },
      { label: 'TypeScript', link: '/sdk/typescript/' },
    ],
  },
  {
    label: 'Axrun',
    items: [
      { label: 'Managed Rollouts', translations: { 'zh-cn': '托管 Rollout' }, link: '/axrun/' },
      { label: 'TaskSets and Local Workflows', translations: { 'zh-cn': 'TaskSet 与本地工作流' }, link: '/axrun/local-workflows/' },
    ],
  },
  {
    label: 'Concepts',
    translations: { 'zh-cn': '概念' },
    items: [
      { label: 'Architecture', translations: { 'zh-cn': '整体架构' }, link: '/architecture/' },
      { label: 'Runtime and Resources', translations: { 'zh-cn': '运行时与资源' }, link: '/architecture/resources/' },
    ],
  },
];

export default defineConfig({
  site: 'https://axern.cofy-x.space',
  output: 'static',
  integrations: [
    mermaid({
      autoTheme: true,
      enableLog: false,
      mermaidConfig: {
        flowchart: { curve: 'basis' },
        fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif',
      },
    }),
    sitemap({ i18n: { defaultLocale: 'en', locales: { en: 'en-US', 'zh-cn': 'zh-CN' } } }),
    starlight({
      title: 'Axern',
      description: 'Open-source AI sandboxes for untrusted code, durable services, and reproducible agent rollouts.',
      favicon: '/favicon.svg',
      customCss: ['./src/styles/custom.css'],
      defaultLocale: 'root',
      locales: {
        root: { label: 'English', lang: 'en' },
        'zh-cn': { label: '简体中文', lang: 'zh-CN' },
      },
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/cofy-x/axern' },
      ],
      editLink: { baseUrl: 'https://github.com/cofy-x/axern/edit/main/apps/docs/' },
      lastUpdated: true,
      pagination: true,
      sidebar,
      head: [
        { tag: 'meta', attrs: { name: 'theme-color', content: '#07111f' } },
        { tag: 'meta', attrs: { property: 'og:site_name', content: 'Axern Documentation' } },
        { tag: 'meta', attrs: { property: 'og:type', content: 'website' } },
        { tag: 'meta', attrs: { property: 'og:image', content: 'https://axern.cofy-x.space/social-card.png' } },
        { tag: 'meta', attrs: { property: 'og:image:type', content: 'image/png' } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
        { tag: 'meta', attrs: { property: 'og:image:alt', content: 'Axern — Infrastructure for AI agents' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: 'https://axern.cofy-x.space/social-card.png' } },
        { tag: 'meta', attrs: { name: 'twitter:image:alt', content: 'Axern — Infrastructure for AI agents' } },
      ],
    }),
  ],
});
