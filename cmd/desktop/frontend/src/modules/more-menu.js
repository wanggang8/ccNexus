/**
 * More Menu Module
 * Handles the "More" dropdown menu functionality
 */

let moreMenuOpen = false;

/**
 * Initialize the more menu
 */
export function initMoreMenu() {
    const moreButton = document.getElementById('more-menu-btn');
    const moreMenu = document.getElementById('more-menu');

    if (!moreButton || !moreMenu) {
        console.warn('More menu elements not found');
        return;
    }

    // Close menu when clicking outside
    document.addEventListener('click', (e) => {
        if (moreMenuOpen && !moreButton.contains(e.target) && !moreMenu.contains(e.target)) {
            closeMoreMenu();
        }
    });

    // Close menu on escape key
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && moreMenuOpen) {
            closeMoreMenu();
        }
    });
}

/**
 * Toggle the more menu
 */
export function toggleMoreMenu() {
    const moreMenu = document.getElementById('more-menu');
    if (!moreMenu) return;

    moreMenuOpen = !moreMenuOpen;
    moreMenu.classList.toggle('show', moreMenuOpen);
}

/**
 * Close the more menu
 */
export function closeMoreMenu() {
    const moreMenu = document.getElementById('more-menu');
    if (!moreMenu) return;

    moreMenuOpen = false;
    moreMenu.classList.remove('show');
}

/**
 * Check if more menu is open
 */
export function isMoreMenuOpen() {
    return moreMenuOpen;
}
