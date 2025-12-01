const fs = require('fs');
const path = require('path');

const rawPath = path.join(__dirname, 'raw.md');
const itemsPath = path.join(__dirname, 'items.json');

try {
    const rawContent = fs.readFileSync(rawPath, 'utf8');
    const lines = rawContent.split('\n');
    const items = [];

    lines.forEach(line => {
        // Regex to match "Name"Count or "Name",Count
        // Handles optional comma, optional spaces, and trailing chars like ✅
        const match = line.match(/"([^"]+)"\s*,?\s*(\d+)/);
        
        if (match) {
            const name = match[1];
            const target = parseInt(match[2], 10);
            const isCompleted = line.includes('✅');
            
            items.push({
                name: name,
                target: target,
                gathered: isCompleted ? target : 0
            });
        }
    });

    fs.writeFileSync(itemsPath, JSON.stringify(items, null, 2));
    console.log(`Translated ${items.length} items from raw.md to items.json`);

} catch (err) {
    console.error('Error translating file:', err);
}

