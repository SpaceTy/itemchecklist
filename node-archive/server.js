const express = require('express');
const bodyParser = require('body-parser');
const cookieParser = require('cookie-parser');
const fs = require('fs');
const path = require('path');

const app = express();
const PORT = 3001;

app.use(bodyParser.json());
app.use(cookieParser());
app.use(express.static('public'));

const CONFIG_PATH = path.join(__dirname, 'config.json');
const ITEMS_PATH = path.join(__dirname, 'items.json');
const BACKUPS_DIR = path.join(__dirname, 'backups');

if (!fs.existsSync(BACKUPS_DIR)) {
    fs.mkdirSync(BACKUPS_DIR);
}

// Helper to read/write JSON
function readJson(filePath) {
    try {
        const data = fs.readFileSync(filePath, 'utf8');
        return JSON.parse(data);
    } catch (e) {
        console.error(`Error reading ${filePath}:`, e);
        return null;
    }
}

function writeJson(filePath, data) {
    try {
        fs.writeFileSync(filePath, JSON.stringify(data, null, 2));
    } catch (e) {
        console.error(`Error writing ${filePath}:`, e);
    }
}

// Backup Logic
function performBackup() {
    const items = readJson(ITEMS_PATH);
    if (items) {
        const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
        const backupPath = path.join(BACKUPS_DIR, `items-${timestamp}.json`);
        writeJson(backupPath, items);
        console.log(`Backup created: ${backupPath}`);
        
        // Cleanup old backups (keep last 50 for example)
        try {
            const files = fs.readdirSync(BACKUPS_DIR)
                .filter(f => f.startsWith('items-'))
                .sort(); // Lexicographical sort works for ISO dates
            
            while (files.length > 50) {
                const toDelete = files.shift();
                fs.unlinkSync(path.join(BACKUPS_DIR, toDelete));
            }
        } catch (e) {
            console.error('Error cleaning up backups:', e);
        }
    }
}

// Schedule backup every 5 minutes
setInterval(performBackup, 5 * 60 * 1000);

// Auth Middleware
function isAuthenticated(req, res, next) {
    const authCookie = req.cookies['auth_token'];
    const config = readJson(CONFIG_PATH);
    
    if (config && config.passwords && config.passwords.includes(authCookie)) {
        next();
    } else {
        res.status(401).json({ error: 'Unauthorized' });
    }
}

// SSE Clients
let clients = [];

function sendEventsToAll(data) {
    clients.forEach(client => client.res.write(`data: ${JSON.stringify(data)}\n\n`));
}

// Routes

// Login
app.post('/api/login', (req, res) => {
    const { password } = req.body;
    const config = readJson(CONFIG_PATH);

    if (config && config.passwords.includes(password)) {
        res.cookie('auth_token', password, { httpOnly: false, maxAge: 30 * 24 * 60 * 60 * 1000 }); // 30 days
        res.json({ success: true });
    } else {
        res.status(401).json({ error: 'Invalid password' });
    }
});

// Check Auth
app.get('/api/check-auth', isAuthenticated, (req, res) => {
    res.json({ success: true });
});

// Get Items
app.get('/api/items', isAuthenticated, (req, res) => {
    const items = readJson(ITEMS_PATH) || [];
    res.json(items);
});

// Update Item
app.post('/api/items/update', isAuthenticated, (req, res) => {
    const { name, gathered } = req.body;
    const items = readJson(ITEMS_PATH) || [];
    
    const itemIndex = items.findIndex(i => i.name === name);
    if (itemIndex > -1) {
        items[itemIndex].gathered = gathered;
        writeJson(ITEMS_PATH, items);
        
        // Notify all clients
        sendEventsToAll({ type: 'update', items: items });
        
        res.json({ success: true });
    } else {
        res.status(404).json({ error: 'Item not found' });
    }
});

// Manage Passwords
app.get('/api/config/passwords', isAuthenticated, (req, res) => {
    const config = readJson(CONFIG_PATH);
    res.json(config.passwords);
});

app.post('/api/config/passwords', isAuthenticated, (req, res) => {
    const { password, action } = req.body; // action: 'add' or 'remove'
    const config = readJson(CONFIG_PATH);
    
    if (action === 'add') {
        if (!config.passwords.includes(password)) {
            config.passwords.push(password);
            writeJson(CONFIG_PATH, config);
            res.json({ success: true, passwords: config.passwords });
        } else {
            res.status(400).json({ error: 'Password already exists' });
        }
    } else if (action === 'remove') {
        const index = config.passwords.indexOf(password);
        if (index > -1) {
            // Prevent removing the last password so we don't lock ourselves out
            if (config.passwords.length <= 1) {
                 return res.status(400).json({ error: 'Cannot remove the last password' });
            }
            config.passwords.splice(index, 1);
            writeJson(CONFIG_PATH, config);
            res.json({ success: true, passwords: config.passwords });
        } else {
            res.status(404).json({ error: 'Password not found' });
        }
    } else {
        res.status(400).json({ error: 'Invalid action' });
    }
});

// SSE Endpoint
app.get('/events', (req, res) => {
    // Ideally verify auth here too, but cookies might not be sent in EventSource in all browsers/contexts easily without withCredentials
    // We'll rely on cookie
    const authCookie = req.cookies['auth_token'];
    const config = readJson(CONFIG_PATH);
    if (!config || !config.passwords.includes(authCookie)) {
        res.status(401).end();
        return;
    }

    const headers = {
        'Content-Type': 'text/event-stream',
        'Connection': 'keep-alive',
        'Cache-Control': 'no-cache'
    };
    res.writeHead(200, headers);

    const clientId = Date.now();
    const newClient = {
        id: clientId,
        res
    };
    clients.push(newClient);

    req.on('close', () => {
        clients = clients.filter(c => c.id !== clientId);
    });
});

app.listen(PORT, () => {
    console.log(`Server running on http://localhost:${PORT} 🚀`);
});
