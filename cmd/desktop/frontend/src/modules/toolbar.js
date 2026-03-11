// 工具栏功能模块 - 处理"更多"下拉菜单等

// 初始化"更多"下拉菜单
export function initMoreDropdown() {
    const moreBtn = document.getElementById('moreDropdownBtn');
    const moreMenu = document.getElementById('moreDropdownMenu');

    if (!moreBtn || !moreMenu) return;

    // 点击按钮切换菜单
    moreBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleMoreMenu();
    });

    // 阻止菜单内点击冒泡
    moreMenu.addEventListener('click', (e) => {
        e.stopPropagation();
    });

    // 点击外部关闭菜单
    document.addEventListener('click', () => {
        closeMoreMenu();
    });

    // 菜单项点击后关闭菜单
    moreMenu.querySelectorAll('.dropdown-item').forEach(item => {
        item.addEventListener('click', () => {
            closeMoreMenu();
        });
    });
}

// 切换"更多"菜单
export function toggleMoreMenu() {
    const moreMenu = document.getElementById('moreDropdownMenu');
    const moreBtn = document.getElementById('moreDropdownBtn');

    if (!moreMenu || !moreBtn) return;

    const isOpen = !moreMenu.classList.contains('hidden');

    if (isOpen) {
        closeMoreMenu();
    } else {
        moreMenu.classList.remove('hidden');
        moreBtn.classList.add('active');
    }
}

// 关闭"更多"菜单
export function closeMoreMenu() {
    const moreMenu = document.getElementById('moreDropdownMenu');
    const moreBtn = document.getElementById('moreDropdownBtn');

    if (moreMenu) {
        moreMenu.classList.add('hidden');
    }
    if (moreBtn) {
        moreBtn.classList.remove('active');
    }
}

// 导出给 window 使用
export function openTerminal() {
    closeMoreMenu();
    if (window.initTerminal) {
        window.initTerminal();
    }
}

export function openDataSync() {
    closeMoreMenu();
    if (window.showDataSyncDialog) {
        window.showDataSyncDialog();
    }
}
