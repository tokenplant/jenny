// ============================================
// GLIMPSE UI — i18n System
// Simple key-value translation with param replacement
// ============================================

export type Locale = 'en' | 'zh-Hans' | 'zh-Hant';

export const DEFAULT_LOCALE: Locale = 'en';
export const SUPPORTED_LOCALES: Locale[] = ['en', 'zh-Hans', 'zh-Hant'];
export const LOCALE_STORAGE_KEY = 'jenny-portal-locale';

export type Messages = Record<string, string>;

const translations: Record<Locale, Messages> = {
  en: {
    // Common
    'common.cancel': 'Cancel',
    'common.confirm': 'Confirm',
    'common.save': 'Save',
    'common.delete': 'Delete',
    'common.edit': 'Edit',
    'common.close': 'Close',
    'common.loading': 'Loading…',
    'common.error': 'Error',
    'common.success': 'Success',
    'common.warning': 'Warning',
    'common.info': 'Info',
    'common.optional': '(optional)',
    'common.required': '(required)',

    // Page
    'page.empty': 'No items yet',
    'page.empty.hint': 'Nothing to show here.',
    'page.error': 'Something went wrong',
    'page.error.retry': 'Try again',

    // Actions
    'action.refresh': 'Refresh',
    'action.expand': 'Expand',
    'action.collapse': 'Collapse',
    'action.copy': 'Copy',
    'action.copied': 'Copied!',
    'action.download': 'Download',

    // Jenny Portal
    'portal.start': 'Start',
    'portal.sessions': 'Sessions',
    'portal.projects': 'Projects',
    'portal.skills': 'Skills',
    'portal.mcp': 'MCP',
    'portal.plugins': 'Plugins',
    'portal.marketplace': 'Marketplace',
    'portal.new_session': 'Start a new session',
    'portal.launch': 'Launch Agent',
    'portal.settings': 'Settings',
    'portal.recent_projects': 'Recent Projects',
    'portal.coming_soon': 'Coming Soon',
    'portal.coming_soon.hint': 'This feature is under development.',
  },
  'zh-Hans': {
    // Common
    'common.cancel': '取消',
    'common.confirm': '确认',
    'common.save': '保存',
    'common.delete': '删除',
    'common.edit': '编辑',
    'common.close': '关闭',
    'common.loading': '加载中…',
    'common.error': '错误',
    'common.success': '成功',
    'common.warning': '警告',
    'common.info': '信息',
    'common.optional': '（可选）',
    'common.required': '（必填）',

    // Page
    'page.empty': '暂无内容',
    'page.empty.hint': '这里什么都没有。',
    'page.error': '出错了',
    'page.error.retry': '重试',

    // Actions
    'action.refresh': '刷新',
    'action.expand': '展开',
    'action.collapse': '收起',
    'action.copy': '复制',
    'action.copied': '已复制！',
    'action.download': '下载',

    // Jenny Portal
    'portal.start': '开始',
    'portal.sessions': '会话',
    'portal.projects': '项目',
    'portal.skills': '技能',
    'portal.mcp': 'MCP',
    'portal.plugins': '插件',
    'portal.marketplace': '市场',
    'portal.new_session': '启动新会话',
    'portal.launch': '启动 Agent',
    'portal.settings': '设置',
    'portal.recent_projects': '最近项目',
    'portal.coming_soon': '敬请期待',
    'portal.coming_soon.hint': '该功能正在开发中。',
  },
  'zh-Hant': {
    // Common
    'common.cancel': '取消',
    'common.confirm': '確認',
    'common.save': '儲存',
    'common.delete': '刪除',
    'common.edit': '編輯',
    'common.close': '關閉',
    'common.loading': '載入中…',
    'common.error': '錯誤',
    'common.success': '成功',
    'common.warning': '警告',
    'common.info': '資訊',
    'common.optional': '（選填）',
    'common.required': '（必填）',

    // Page
    'page.empty': '尚無項目',
    'page.empty.hint': '這裡沒有內容。',
    'page.error': '發生錯誤',
    'page.error.retry': '重試',

    // Actions
    'action.refresh': '重新整理',
    'action.expand': '展開',
    'action.collapse': '收合',
    'action.copy': '複製',
    'action.copied': '已複製！',
    'action.download': '下載',

    // Jenny Portal
    'portal.start': '開始',
    'portal.sessions': '會話',
    'portal.projects': '專案',
    'portal.skills': '技能',
    'portal.mcp': 'MCP',
    'portal.plugins': '插件',
    'portal.marketplace': '市集',
    'portal.new_session': '啟動新會話',
    'portal.launch': '啟動 Agent',
    'portal.settings': '設定',
    'portal.recent_projects': '最近專案',
    'portal.coming_soon': '即將推出',
    'portal.coming_soon.hint': '此功能正在開發中。',
  },
};

function replaceParams(template: string, params: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (_, key) => String(params[key] ?? `{${key}}`));
}

/**
 * createTranslator — Creates a translator function for a given locale
 */
export function createTranslator(locale: Locale) {
  const msgs = translations[locale] ?? translations[DEFAULT_LOCALE];

  return function t(key: string, params?: Record<string, string | number>): string {
    const template = msgs[key] ?? key;
    return params ? replaceParams(template, params) : template;
  };
}

export type Translator = ReturnType<typeof createTranslator>;

/**
 * assertKeyParity — Ensure all locales have the same keys
 */
export function assertKeyParity(): void {
  const keysByLocale: Record<Locale, Set<string>> = {
    en: new Set(Object.keys(translations.en)),
    'zh-Hans': new Set(Object.keys(translations['zh-Hans'])),
    'zh-Hant': new Set(Object.keys(translations['zh-Hant'])),
  };

  for (const locale of SUPPORTED_LOCALES) {
    const missing = [...keysByLocale[locale]].filter(
      (k) => !keysByLocale[DEFAULT_LOCALE].has(k)
    );
    const extra = [...keysByLocale[DEFAULT_LOCALE]].filter(
      (k) => !keysByLocale[locale].has(k)
    );

    if (missing.length > 0) {
      console.warn(`[i18n] Missing keys in "${locale}":`, missing);
    }
    if (extra.length > 0) {
      console.warn(`[i18n] Extra keys in "${locale}":`, extra);
    }
  }
}