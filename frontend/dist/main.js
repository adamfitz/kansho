// Import Wails runtime - this will be available when built with Wails
// In development, you might need to mock these functions

let clickCounter = 0;

// Initialize the application
document.addEventListener('DOMContentLoaded', function() {
    console.log('Wails GUI Example loaded');
    setupEventListeners();
});

function setupEventListeners() {
    // Greeting functionality
    const greetBtn = document.getElementById('greetBtn');
    const nameInput = document.getElementById('name');
    const greetResult = document.getElementById('greetResult');

    greetBtn.addEventListener('click', async () => {
        const name = nameInput.value || 'World';
        try {
            greetResult.textContent = 'Loading...';
            // Call Go backend function
            const result = await window.go.main.App.Greet(name);
            greetResult.textContent = result;
        } catch (error) {
            greetResult.textContent = `Error: ${error.message}`;
        }
    });

    // Allow Enter key to trigger greeting
    nameInput.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') {
            greetBtn.click();
        }
    });

    // System info functionality
    const timeBtn = document.getElementById('timeBtn');
    const sysInfoBtn = document.getElementById('sysInfoBtn');
    const sysInfoResult = document.getElementById('sysInfoResult');

    timeBtn.addEventListener('click', async () => {
        try {
            sysInfoResult.textContent = 'Getting time...';
            const time = await window.go.main.App.GetCurrentTime();
            sysInfoResult.textContent = `Current Time: ${time}`;
        } catch (error) {
            sysInfoResult.textContent = `Error: ${error.message}`;
        }
    });

    sysInfoBtn.addEventListener('click', async () => {
        try {
            sysInfoResult.textContent = 'Getting system info...';
            const info = await window.go.main.App.GetSystemInfo();
            sysInfoResult.textContent = `Platform: ${info.OS}\nArchitecture: ${info.Arch}`;
        } catch (error) {
            sysInfoResult.textContent = `Error: ${error.message}`;
        }
    });

    // Dialog functionality
    const messageBtn = document.getElementById('messageBtn');
    const fileBtn = document.getElementById('fileBtn');
    const dialogResult = document.getElementById('dialogResult');

    messageBtn.addEventListener('click', async () => {
        try {
            await window.go.main.App.ShowMessage('Hello!', 'This is a message from the Wails GUI example!');
            dialogResult.textContent = 'Message dialog shown successfully!';
        } catch (error) {
            dialogResult.textContent = `Error: ${error.message}`;
        }
    });

    fileBtn.addEventListener('click', async () => {
        try {
            dialogResult.textContent = 'Opening file dialog...';
            const result = await window.go.main.App.ShowOpenDialog();
            dialogResult.textContent = result;
        } catch (error) {
            dialogResult.textContent = `Error: ${error.message}`;
        }
    });

    // Interactive demo functionality
    const counterBtn = document.getElementById('counterBtn');
    const resetBtn = document.getElementById('resetBtn');
    const counterSpan = document.getElementById('counter');

    counterBtn.addEventListener('click', () => {
        clickCounter++;
        counterSpan.textContent = clickCounter;
        
        // Add some visual feedback
        counterBtn.style.transform = 'scale(0.95)';
        setTimeout(() => {
            counterBtn.style.transform = 'scale(1)';
        }, 150);
    });

    resetBtn.addEventListener('click', () => {
        clickCounter = 0;
        counterSpan.textContent = clickCounter;
        
        // Reset progress bar too
        const progressRange = document.getElementById('progressRange');
        const progressFill = document.getElementById('progressFill');
        const progressValue = document.getElementById('progressValue');
        
        progressRange.value = 0;
        progressFill.style.width = '0%';
        progressValue.textContent = '0%';
    });

    // Progress bar functionality
    const progressRange = document.getElementById('progressRange');
    const progressFill = document.getElementById('progressFill');
    const progressValue = document.getElementById('progressValue');

    progressRange.addEventListener('input', (e) => {
        const value = e.target.value;
        progressFill.style.width = `${value}%`;
        progressValue.textContent = `${value}%`;
    });

    // Add some keyboard shortcuts
    document.addEventListener('keydown', (e) => {
        // Ctrl/Cmd + G for greet
        if ((e.ctrlKey || e.metaKey) && e.key === 'g') {
            e.preventDefault();
            greetBtn.click();
        }
        
        // Ctrl/Cmd + T for time
        if ((e.ctrlKey || e.metaKey) && e.key === 't') {
            e.preventDefault();
            timeBtn.click();
        }
        
        // Space for counter
        if (e.code === 'Space' && e.target.tagName !== 'INPUT') {
            e.preventDefault();
            counterBtn.click();
        }
    });

    // Add some visual enhancements
    addVisualEnhancements();
}

function addVisualEnhancements() {
    // Add ripple effect to buttons
    const buttons = document.querySelectorAll('.btn');
    buttons.forEach(button => {
        button.addEventListener('click', function(e) {
            const ripple = document.createElement('span');
            const rect = this.getBoundingClientRect();
            const size = Math.max(rect.width, rect.height);
            const x = e.clientX - rect.left - size / 2;
            const y = e.clientY - rect.top - size / 2;
            
            ripple.style.width = ripple.style.height = size + 'px';
            ripple.style.left = x + 'px';
            ripple.style.top = y + 'px';
            ripple.classList.add('ripple');
            
            this.appendChild(ripple);
            
            setTimeout(() => {
                ripple.remove();
            }, 600);
        });
    });

    // Add CSS for ripple effect
    const style = document.createElement('style');
    style.textContent = `
        .btn {
            position: relative;
            overflow: hidden;
        }
        
        .ripple {
            position: absolute;
            border-radius: 50%;
            background: rgba(255, 255, 255, 0.3);
            transform: scale(0);
            animation: ripple-animation 0.6s linear;
            pointer-events: none;
        }
        
        @keyframes ripple-animation {
            to {
                transform: scale(2);
                opacity: 0;
            }
        }
    `;
    document.head.appendChild(style);

    // Auto-update time every second when displayed
    let timeInterval;
    const timeBtn = document.getElementById('timeBtn');
    const sysInfoResult = document.getElementById('sysInfoResult');
    
    const originalTimeClick = timeBtn.onclick;
    timeBtn.addEventListener('click', () => {
        if (timeInterval) {
            clearInterval(timeInterval);
        }
        
        timeInterval = setInterval(async () => {
            try {
                const time = await window.go.main.App.GetCurrentTime();
                if (sysInfoResult.textContent.includes('Current Time:')) {
                    sysInfoResult.textContent = `Current Time: ${time}`;
                }
            } catch (error) {
                clearInterval(timeInterval);
            }
        }, 1000);
        
        // Clear interval after 30 seconds to avoid unnecessary updates
        setTimeout(() => {
            if (timeInterval) {
                clearInterval(timeInterval);
                timeInterval = null;
            }
        }, 30000);
    });
}

// Handle window events
window.addEventListener('beforeunload', () => {
    console.log('Application closing...');
});

// Error handling for missing Wails runtime
if (typeof window.go === 'undefined') {
    console.warn('Wails runtime not found. Running in development mode.');
    
    // Mock the Go functions for development
    window.go = {
        main: {
            App: {
                Greet: (name) => Promise.resolve(`Hello ${name}! (Development Mode)`),
                GetCurrentTime: () => Promise.resolve(new Date().toLocaleString()),
                GetSystemInfo: () => Promise.resolve({
                    OS: 'Development',
                    Arch: 'mock'
                }),
                ShowMessage: (title, message) => {
                    alert(`${title}: ${message}`);
                    return Promise.resolve();
                },
                ShowOpenDialog: () => Promise.resolve('No file selected (Development Mode)')
            }
        }
    };
}