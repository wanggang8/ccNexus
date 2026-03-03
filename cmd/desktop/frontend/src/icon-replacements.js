// UI Icon Replacements
// This module provides helper functions to replace emoji with SVG icons

import { getIcon } from './icons.js';

// Icon mapping from emoji to icon names
export const iconMap = {
  '🚀': 'rocket',
  '📊': 'chart',
  '📅': 'calendar',
  '📆': 'calendar',
  '📈': 'trendUp',
  '📚': 'activity',
  '🔌': 'server',
  '💻': 'terminal',
  '📝': 'log',
  '📡': 'network',
  '⚙️': 'settings',
  '📖': 'book',
  '➕': 'plus',
  '✏️': 'edit',
  '🗑️': 'trash',
  '✅': 'check',
  '❌': 'x',
  '⚠️': 'alert',
  'ℹ️': 'info',
  '👁️': 'eye',
  '📋': 'copy',
  '⬇️': 'download',
  '⬆️': 'upload',
  '🔄': 'refresh',
  '🔍': 'search',
  '🎯': 'filter',
  '❤️': 'heart',
  '⚡': 'zap',
  '🕐': 'clock',
  '📦': 'package',
};

// Replace emoji in text with icon HTML
export function replaceEmoji(text, iconClass = '') {
  let result = text;
  for (const [emoji, iconName] of Object.entries(iconMap)) {
    if (result.includes(emoji)) {
      const iconHtml = `<span class="icon ${iconClass}">${getIcon(iconName)}</span>`;
      result = result.replace(new RegExp(emoji, 'g'), iconHtml);
    }
  }
  return result;
}

// Create icon element with optional text
export function createIconWithText(iconName, text, iconClass = '') {
  return `<span class="icon ${iconClass}">${getIcon(iconName)}</span>${text ? ` ${text}` : ''}`;
}

// Replace emoji in element
export function replaceEmojiInElement(element) {
  if (!element) return;

  const walker = document.createTreeWalker(
    element,
    NodeFilter.SHOW_TEXT,
    null,
    false
  );

  const nodesToReplace = [];
  let node;

  while (node = walker.nextNode()) {
    for (const emoji of Object.keys(iconMap)) {
      if (node.textContent.includes(emoji)) {
        nodesToReplace.push(node);
        break;
      }
    }
  }

  nodesToReplace.forEach(node => {
    const span = document.createElement('span');
    span.innerHTML = replaceEmoji(node.textContent);
    node.parentNode.replaceChild(span, node);
  });
}
