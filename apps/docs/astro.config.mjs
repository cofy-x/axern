import sitemap from '@astrojs/sitemap';
import starlight from '@astrojs/starlight';
import { defineConfig } from 'astro/config';
import mermaid from 'astro-mermaid';

const sidebar = [
  {
    label: 'Start',
    translations: { 'zh-CN': '开始' },
    items: [
      { label: 'Overview', translations: { 'zh-CN': '产品概览' }, link: '/' },
      { label: 'Quick Start', translations: { 'zh-CN': '快速开始' }, link: '/getting-started/' },
      { label: 'Local Axern', translations: { 'zh-CN': 'Local Axern' }, link: '/getting-started/compose/' },
      { label: 'Kubernetes Install', translations: { 'zh-CN': 'Kubernetes 安装' }, link: '/getting-started/kubernetes/' },
    ],
  },
  {
    label: 'Guides',
    translations: { 'zh-CN': '指南' },
    items: [
      {
        label: 'Tools',
        translations: { 'zh-CN': '工具' },
        items: [
          { label: 'Axern CLI', translations: { 'zh-CN': 'Axern CLI' }, link: '/guides/cli/' },
          { label: 'Local Axern Reference', translations: { 'zh-CN': 'Local Axern 参考' }, link: '/guides/local/' },
          { label: 'Upgrades and Versioning', translations: { 'zh-CN': '升级与版本' }, link: '/guides/upgrades/' },
        ],
      },
      {
        label: 'Workloads',
        translations: { 'zh-CN': '工作负载' },
        items: [
          { label: 'Runs', translations: { 'zh-CN': 'Run' }, link: '/guides/run/' },
          { label: 'Services', translations: { 'zh-CN': 'Service' }, link: '/guides/service/' },
          { label: 'Functions', translations: { 'zh-CN': 'Function' }, link: '/guides/functions/' },
          { label: 'Coding Agents', translations: { 'zh-CN': '编码 Agent' }, link: '/guides/agent/' },
        ],
      },
      {
        label: 'SDK Guides',
        translations: { 'zh-CN': 'SDK 指南' },
        items: [
          { label: 'Python Service', translations: { 'zh-CN': 'Python Service' }, link: '/guides/python-service/' },
          { label: 'Computer Use and Browser', translations: { 'zh-CN': 'Computer Use 与浏览器' }, link: '/guides/computer-use/' },
        ],
      },
      {
        label: 'Data and Config',
        translations: { 'zh-CN': '数据与配置' },
        items: [
          { label: 'Catalog', translations: { 'zh-CN': 'Catalog' }, link: '/guides/catalog/' },
          { label: 'Environments and Quota', translations: { 'zh-CN': '环境与配额' }, link: '/guides/environments/' },
          { label: 'Secrets', translations: { 'zh-CN': 'Secret' }, link: '/guides/secrets/' },
          { label: 'Storage and Volumes', translations: { 'zh-CN': '存储与卷' }, link: '/guides/storage/' },
        ],
      },
      {
        label: 'Identity and Access',
        translations: { 'zh-CN': '身份与访问' },
        items: [
          { label: 'Namespace Access', translations: { 'zh-CN': '命名空间访问' }, link: '/guides/authorization/' },
          { label: 'SSH Access', translations: { 'zh-CN': 'SSH 访问' }, link: '/guides/ssh/' },
          { label: 'Reverse Tunnels', translations: { 'zh-CN': '反向隧道' }, link: '/guides/tunnels/' },
        ],
      },
      { label: 'Troubleshooting', translations: { 'zh-CN': '故障排查' }, link: '/guides/troubleshooting/' },
    ],
  },
  {
    label: 'SDKs',
    translations: { 'zh-CN': 'SDK' },
    items: [
      { label: 'Overview', translations: { 'zh-CN': '概览' }, link: '/sdk/' },
      { label: 'Python', link: '/sdk/python/' },
      { label: 'Go', link: '/sdk/go/' },
      { label: 'TypeScript', link: '/sdk/typescript/' },
    ],
  },
  {
    label: 'Axrun',
    translations: { 'zh-CN': 'Axrun' },
    items: [
      { label: 'Managed Rollouts', translations: { 'zh-CN': '托管 Rollout' }, link: '/axrun/' },
      { label: 'TaskSets and Local Workflows', translations: { 'zh-CN': 'TaskSet 与本地工作流' }, link: '/axrun/local-workflows/' },
    ],
  },
  {
    label: 'Concepts',
    translations: { 'zh-CN': '概念' },
    items: [
      { label: 'Architecture', translations: { 'zh-CN': '整体架构' }, link: '/architecture/' },
      { label: 'Sandbox Model', translations: { 'zh-CN': 'Sandbox 模型' }, link: '/architecture/sandbox-model/' },
      { label: 'Runtime and Resources', translations: { 'zh-CN': '运行时与资源' }, link: '/architecture/resources/' },
      { label: 'Node Networking', translations: { 'zh-CN': '节点网络' }, link: '/architecture/networking/' },
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
